package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/mkassab215/lazycake/internal/clock"
	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/billing"
	"github.com/mkassab215/lazycake/internal/coordinator/config"
	"github.com/mkassab215/lazycake/internal/coordinator/dashboard"
	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/portalapi"
	coordrelay "github.com/mkassab215/lazycake/internal/coordinator/relay"
	"github.com/mkassab215/lazycake/internal/coordinator/scheduler"
	"github.com/mkassab215/lazycake/internal/coordinator/seed"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/logging"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

const (
	heartbeatS = 15
	leaseS     = 60
)

// gatewayFanout routes GatewayService.ReportBytes to both byte
// reconciliation (task 4.3) and canary detection (task 5.2) - the same
// one gateway report is proof relevant to both.
type gatewayFanout struct {
	reconciler *billing.Reconciler
	canaries   *scheduler.CanaryTracker
}

func (f gatewayFanout) RecordGatewayBytes(taskID string, bytes int64) {
	f.reconciler.RecordGatewayBytes(taskID, bytes)
	f.canaries.RecordGatewayActivity(taskID)
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(nil)
	if err != nil {
		return err
	}

	log := logging.New(os.Stderr, cfg.Dev)
	log.Info("coordinator starting", "config", cfg)

	st, err := store.NewPostgresStore(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connecting to store: %w", err)
	}
	defer st.Close()

	if cfg.SeedDemoToken != "" {
		if err := seed.EnsureDemoAccount(ctx, st, cfg.SeedDemoToken); err != nil {
			return fmt.Errorf("seeding demo account: %w", err)
		}
		log.Info("seeded demo account act_demo")
	}

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.GRPCAddr, err)
	}

	// Bus fans coordinator activity out to task 6.1's SSE stream. It's a
	// pure observer - nothing downstream of it affects dispatch,
	// billing, or trust - so it's wired everywhere last and freely.
	bus := events.NewBus()

	registry := api.NewRegistry()
	sched := scheduler.New(st, registry, clock.Real{}, log, leaseS)
	sched.Trust.Store = st
	sched.Trust.Log = log
	sched.Bus = bus

	// Canary injection (task 5.2). The account/gateway row always exist -
	// seeding is idempotent and cheap - but injection only does anything
	// useful once an operator actually runs a gateway process under
	// platformGatewayID; until then it fails closed (see
	// seed.EnsurePlatformCanaryAccount's own doc comment).
	platformAccountID, platformGatewayID, err := seed.EnsurePlatformCanaryAccount(ctx, st)
	if err != nil {
		return fmt.Errorf("seeding platform canary account: %w", err)
	}
	sched.PlatformAccountID = platformAccountID
	sched.PlatformGatewayID = platformGatewayID
	// CanaryImage is deliberately left unset by default: task 5.2's
	// workload has to be a real digest-pinned image an operator actually
	// builds and publishes (a small program that connects to
	// CanaryTargetHostname:CanaryTargetPort and produces a known,
	// verifiable result - PLAN.md's "known expected output hash" is not
	// implemented in this codebase, see OPEN_QUESTIONS.md), which is a
	// deployment decision this code has no business making up. Injection
	// (maybeDispatchCanary) checks CanaryImage != "" and no-ops until one
	// is configured, same fail-closed posture as the gateway process
	// itself. Set via LAZYCAKE_CANARY_* if/when a real workload exists.
	sched.CanaryImage = os.Getenv("LAZYCAKE_CANARY_IMAGE")
	sched.CanaryEntrypoint = splitNonEmpty(os.Getenv("LAZYCAKE_CANARY_ENTRYPOINT"))
	sched.CanaryArgs = splitNonEmpty(os.Getenv("LAZYCAKE_CANARY_ARGS"))
	sched.CanaryTargetHostname = envOr("LAZYCAKE_CANARY_TARGET_HOSTNAME", "canary-target")
	sched.CanaryTargetPort = 7
	sched.CanaryExpectedRuntimeS = 10

	// Three-point byte reconciliation (task 4.3): the relay's own stream
	// counters and the gateway's ReportBytes RPC both feed the same
	// Reconciler the agent's TaskFinished report does (via billing.Meters
	// below), so all three independently-observed byte counts for one
	// task end up in one place to compare.
	reconciler := &billing.Reconciler{
		ResolveNodeID: func(ctx context.Context, taskID string) (string, error) {
			t, err := st.GetTask(ctx, taskID)
			if err != nil {
				return "", err
			}
			if t.NodeID == nil {
				return "", nil
			}
			return *t.NodeID, nil
		},
		Log: log,
		// Byte divergence drops trust (task 5.3).
		OnFlagged: func(nodeID, taskID string, rec billing.Reconciliation) {
			sched.Trust.ByteDivergence(nodeID)
		},
	}
	// One Rates value shared by the dispatch-time hold, the submission-time
	// affordability check, and the eventual settlement (task 4.5), so all
	// three always agree on what a task could cost.
	rates := billing.DefaultRates()
	ledger := &billing.Ledger{Store: st, Rates: rates, Log: log}
	sched.Billing = &billing.Meters{Store: st, Clock: clock.Real{}, Log: log, Reconciler: reconciler, Ledger: ledger}
	sched.Rates = rates

	grpcServer := grpc.NewServer()
	lazycakev1.RegisterAgentServiceServer(grpcServer, &api.Server{
		Store:      st,
		Registry:   registry,
		Events:     sched,
		Capacity:   sched,
		Clock:      clock.Real{},
		Log:        log,
		HeartbeatS: heartbeatS,
		LeaseS:     leaseS,
		Bus:        bus,
	})
	customerServer := &api.CustomerServer{Store: st, Rates: rates, Bus: bus}
	lazycakev1.RegisterCustomerServiceServer(grpcServer, customerServer)
	lazycakev1.RegisterGatewayServiceServer(grpcServer, &api.GatewayServer{Store: st, Events: gatewayFanout{reconciler, sched.Canary}})

	go sched.Run(ctx, time.Second)

	serveErr := make(chan error, 1)
	go func() { serveErr <- grpcServer.Serve(lis) }()
	log.Info("agent gRPC listening", "addr", cfg.GRPCAddr)

	tunnelRelay := &quic.Relay{
		Auth:     coordrelay.StoreAdapter{Store: st},
		Gateways: coordrelay.StoreAdapter{Store: st},
		Log:      log,
		OnStreamClosed: func(stats quic.StreamStats) {
			reconciler.RecordRelayBytes(stats.TaskID, stats.BytesAgentToGW+stats.BytesGWToAgent)
		},
	}
	relayErr := make(chan error, 1)
	go func() { relayErr <- tunnelRelay.Serve(ctx, cfg.RelayAddr) }()
	log.Info("tunnel relay listening", "addr", cfg.RelayAddr)

	dash := &dashboard.Server{Store: st, Bus: bus, Trust: sched.Trust, Log: log}
	portal := &portalapi.Server{
		Store: st, Customer: customerServer, Bus: bus, Log: log,
		Dev: cfg.Dev, CoordinatorAddr: cfg.PublicGRPCAddr,
	}
	httpMux := http.NewServeMux()
	httpMux.Handle("/api/portal/", portal.Handler())
	httpMux.Handle("/", dash.Handler())
	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: httpMux}
	httpErr := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			httpErr <- err
		}
	}()
	log.Info("dashboard/portal API listening", "addr", cfg.HTTPAddr)

	select {
	case <-ctx.Done():
		log.Info("coordinator shutting down")
		grpcServer.GracefulStop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return nil
	case err := <-serveErr:
		return fmt.Errorf("grpc server: %w", err)
	case err := <-relayErr:
		return fmt.Errorf("tunnel relay: %w", err)
	case err := <-httpErr:
		return fmt.Errorf("dashboard/portal API server: %w", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// splitNonEmpty splits a comma-separated env value into a slice, or nil if
// it's empty - distinguishing "not configured" from "configured as an
// empty list" matters for scheduler.Scheduler.CanaryEntrypoint/CanaryArgs.
func splitNonEmpty(v string) []string {
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}
