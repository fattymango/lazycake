package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/fattymango/lazycake/internal/gateway/config"
	"github.com/fattymango/lazycake/internal/gateway/listener"
	"github.com/fattymango/lazycake/internal/logging"
	lazycakev1 "github.com/fattymango/lazycake/internal/proto/lazycake/v1"
	"github.com/fattymango/lazycake/internal/tunnel/noise"
	"github.com/fattymango/lazycake/internal/tunnel/quic"
	"github.com/fattymango/lazycake/internal/version"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(nil)
	if err != nil {
		return err
	}

	log := logging.New(os.Stderr, cfg.Dev)
	log.Info("gateway starting", "config", cfg)

	keypair, err := loadOrCreateKeypair(keyPath())
	if err != nil {
		return fmt.Errorf("loading noise keypair: %w", err)
	}
	log.Info("noise static public key", "pubkey_hex", fmt.Sprintf("%x", keypair.Public))

	services := make(map[string]string, len(cfg.Services))
	for _, s := range cfg.Services {
		services[s.Name] = s.Addr()
	}

	// A gateway the customer deleted never serves again, even after a restart of its container
	// (--restart=always brings it back, and the key volume remembers).
	tombstone := tombstonePath(keyPath())
	if revokedBefore(tombstone, cfg.GatewayID) {
		return dormant(ctx, log, cfg.GatewayID)
	}

	reportBytes, hello := newByteReporter(ctx, cfg, log)
	err = serveForever(ctx, log, func(ctx context.Context) error {
		return serveOnce(ctx, cfg, keypair, services, log, reportBytes, hello)
	})
	if errors.Is(err, errGatewayDeleted) {
		if werr := os.WriteFile(tombstone, []byte(cfg.GatewayID), 0o600); werr != nil {
			log.Warn("couldn't record the deletion; a restart will be rejected again instead", "error", werr)
		}
		return dormant(ctx, log, cfg.GatewayID)
	}
	return err
}

// errGatewayDeleted means the coordinator told this gateway it was deleted: its token is expired and
// every connection attempt will be refused, so retrying is pointless.
var errGatewayDeleted = errors.New("gateway was deleted in the dashboard")

func tombstonePath(keyPath string) string { return filepath.Join(filepath.Dir(keyPath), "deleted") }

func revokedBefore(path, gatewayID string) bool {
	b, err := os.ReadFile(path)
	return err == nil && strings.TrimSpace(string(b)) == gatewayID
}

// dormant keeps the process idle until it is stopped: no connections, no forwarding. The container keeps its
// restart policy, so exiting would only have it restarted; the dashboard hands the owner the command that
// removes it. Services stay unreachable either way.
func dormant(ctx context.Context, log *slog.Logger, gatewayID string) error {
	log.Error("this gateway was deleted in the LazyCake dashboard and will not connect again. Remove it with: "+
		"podman rm -f lazycake-gateway && podman volume rm lazycake-gateway-data", "gateway_id", gatewayID)
	<-ctx.Done()
	return nil
}

// serveOnce holds one relay connection: dials, then accepts tunnelled
// streams until the connection or ctx ends.
func serveOnce(ctx context.Context, cfg config.Config, keypair noise.Keypair, services map[string]string,
	log *slog.Logger, reportBytes func(listener.ForwardReport), hello func(context.Context)) error {
	conn, err := quic.DialGateway(ctx, cfg.CoordinatorAddr, cfg.Token, cfg.GatewayID, keypair.Public)
	if err != nil {
		return fmt.Errorf("connecting to relay: %w", err)
	}
	defer conn.CloseWithError(0, "gateway disconnecting")
	log.Info("connected to relay", "addr", cfg.CoordinatorAddr)
	go hello(ctx) // tell the coordinator which release this is (best effort)

	l := &listener.Listener{
		Conn: conn, Keypair: keypair, Services: services, Log: log,
		OnForward: func(s listener.ForwardStats) {
			log.Info("forward closed", "task_id", s.TaskID, "service", s.Service,
				"bytes_to_local", s.BytesToLocal, "bytes_to_task", s.BytesToTask)
		},
		OnReport: reportBytes,
		Limit:    listener.NewBandwidthLimiter(cfg.MaxMbps),
	}
	return l.Run(ctx)
}

// Vars rather than consts so tests can shrink them.
var (
	reconnectMin = time.Second
	reconnectMax = 30 * time.Second
	// healthyAfter is how long a connection must have lasted for the next
	// failure to start over from reconnectMin rather than keep backing off.
	healthyAfter = 30 * time.Second
)

// serveForever keeps a gateway attached to the relay: when the connection
// drops (the coordinator restarting, a network blip) it reconnects with
// capped exponential backoff instead of exiting, so a gateway doesn't
// depend on a process supervisor to come back. It returns only when ctx is
// cancelled.
func serveForever(ctx context.Context, log *slog.Logger, serve func(context.Context) error) error {
	delay := reconnectMin
	for {
		start := time.Now()
		err := serve(ctx)
		if ctx.Err() != nil {
			log.Info("gateway shutting down")
			return nil
		}
		if quic.IsGatewayRevoked(err) {
			return errGatewayDeleted
		}
		if time.Since(start) >= healthyAfter {
			delay = reconnectMin
		}
		log.Warn("relay connection lost, reconnecting", "error", err, "retry_in", delay.String())
		select {
		case <-ctx.Done():
			log.Info("gateway shutting down")
			return nil
		case <-time.After(delay):
		}
		if delay *= 2; delay > reconnectMax {
			delay = reconnectMax
		}
	}
}

// keyPath is where the gateway's Noise static key persists across
// restarts, so its public key (published to the coordinator once, at
// registration - see internal/coordinator/api/customer_server.go and
// PLAN.md "static keys exchanged at gateway registration") doesn't rotate
// out from under an already-configured task every time the process
// restarts.
func keyPath() string {
	if p := os.Getenv("LAZYCAKE_GATEWAY_KEY_PATH"); p != "" {
		return p
	}
	return "lazycake-gateway.key"
}

func loadOrCreateKeypair(path string) (noise.Keypair, error) {
	if data, err := os.ReadFile(path); err == nil {
		priv := make([]byte, len(data))
		copy(priv, data)
		pub, err := noise.PublicFromPrivate(priv)
		if err != nil {
			return noise.Keypair{}, fmt.Errorf("deriving public key from %s: %w", path, err)
		}
		return noise.Keypair{Public: pub, Private: priv}, nil
	}

	kp, err := noise.GenerateKeypair()
	if err != nil {
		return noise.Keypair{}, err
	}
	if err := os.WriteFile(path, kp.Private, 0o600); err != nil {
		return noise.Keypair{}, fmt.Errorf("writing %s: %w", path, err)
	}
	return kp, nil
}

// newByteReporter returns a function that reports one forwarded connection's
// byte-count deltas to the coordinator's GatewayService (billing's third
// reconciliation point, and the per-gateway/per-task traffic statistics), or a
// no-op if cfg.GRPCAddr wasn't set - reporting is defense in depth, not
// something the gateway's actual job depends on, so it degrades gracefully
// rather than failing startup.
func newByteReporter(ctx context.Context, cfg config.Config, log *slog.Logger) (func(listener.ForwardReport), func(context.Context)) {
	if cfg.GRPCAddr == "" {
		log.Warn("LAZYCAKE_GRPC_ADDR not set, byte reconciliation reports disabled")
		return func(listener.ForwardReport) {}, func(context.Context) {}
	}

	conn, err := grpc.NewClient(cfg.GRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Warn("dialing coordinator gRPC for byte reports, disabling them", "addr", cfg.GRPCAddr, "error", err)
		return func(listener.ForwardReport) {}, func(context.Context) {}
	}
	client := lazycakev1.NewGatewayServiceClient(conn)

	rp := newReporter(cfg.GatewayID, log, func(r *lazycakev1.ByteReport) error {
		callCtx, cancel := context.WithTimeout(
			metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+cfg.Token)),
			10*time.Second)
		defer cancel()
		_, err := client.ReportBytes(callCtx, r)
		return err
	})
	go rp.Run(ctx)
	hello := func(hctx context.Context) {
		callCtx, cancel := context.WithTimeout(
			metadata.NewOutgoingContext(hctx, metadata.Pairs("authorization", "Bearer "+cfg.Token)), 10*time.Second)
		defer cancel()
		if _, err := client.Hello(callCtx, &lazycakev1.GatewayHello{GatewayId: cfg.GatewayID, Version: version.Version}); err != nil {
			log.Debug("telling the coordinator this gateway's version", "error", err) // an older coordinator has no such call
		}
	}
	return rp.Report, hello
}
