package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/mkassab215/lazycake/internal/clock"
	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/billing"
	"github.com/mkassab215/lazycake/internal/coordinator/config"
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

	registry := api.NewRegistry()
	sched := scheduler.New(st, registry, clock.Real{}, log, leaseS)

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
	}
	ledger := &billing.Ledger{Store: st, Rates: billing.DefaultRates(), Log: log}
	sched.Billing = &billing.Meters{Store: st, Clock: clock.Real{}, Log: log, Reconciler: reconciler, Ledger: ledger}

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
	})
	lazycakev1.RegisterCustomerServiceServer(grpcServer, &api.CustomerServer{Store: st})
	lazycakev1.RegisterGatewayServiceServer(grpcServer, &api.GatewayServer{Store: st, Events: reconciler})

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

	select {
	case <-ctx.Done():
		log.Info("coordinator shutting down")
		grpcServer.GracefulStop()
		return nil
	case err := <-serveErr:
		return fmt.Errorf("grpc server: %w", err)
	case err := <-relayErr:
		return fmt.Errorf("tunnel relay: %w", err)
	}
}
