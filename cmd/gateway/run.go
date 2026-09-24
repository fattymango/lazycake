package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/mkassab215/lazycake/internal/gateway/config"
	"github.com/mkassab215/lazycake/internal/gateway/listener"
	"github.com/mkassab215/lazycake/internal/logging"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
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

	conn, err := quic.DialGateway(ctx, cfg.CoordinatorAddr, cfg.Token, cfg.GatewayID, keypair.Public)
	if err != nil {
		return fmt.Errorf("connecting to relay: %w", err)
	}

	reportBytes := newByteReporter(cfg, log)

	l := &listener.Listener{
		Conn: conn, Keypair: keypair, Services: services, Log: log,
		OnForward: func(s listener.ForwardStats) {
			log.Info("forward closed", "task_id", s.TaskID, "service", s.Service,
				"bytes_to_local", s.BytesToLocal, "bytes_to_task", s.BytesToTask)
			reportBytes(s)
		},
	}

	errCh := make(chan error, 1)
	go func() { errCh <- l.Run(ctx) }()

	select {
	case <-ctx.Done():
		log.Info("gateway shutting down")
		return nil
	case err := <-errCh:
		return err
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

// newByteReporter returns a function that reports one forwarded
// connection's byte counts to the coordinator's GatewayService (task
// 4.3's third reconciliation point), or a no-op if cfg.GRPCAddr wasn't
// set - reporting is defense in depth for billing, not something the
// gateway's actual job depends on, so it degrades gracefully rather than
// failing startup.
func newByteReporter(cfg config.Config, log *slog.Logger) func(listener.ForwardStats) {
	if cfg.GRPCAddr == "" {
		log.Warn("LAZYCAKE_GRPC_ADDR not set, byte reconciliation reports disabled")
		return func(listener.ForwardStats) {}
	}

	conn, err := grpc.NewClient(cfg.GRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Warn("dialing coordinator gRPC for byte reports, disabling them", "addr", cfg.GRPCAddr, "error", err)
		return func(listener.ForwardStats) {}
	}
	client := lazycakev1.NewGatewayServiceClient(conn)

	return func(s listener.ForwardStats) {
		ctx, cancel := context.WithTimeout(
			metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+cfg.Token)),
			10*time.Second)
		defer cancel()
		_, err := client.ReportBytes(ctx, &lazycakev1.ByteReport{
			GatewayId: cfg.GatewayID, TaskId: s.TaskID,
			BytesToLocal: s.BytesToLocal, BytesToTask: s.BytesToTask,
		})
		if err != nil {
			log.Warn("reporting byte counts", "task_id", s.TaskID, "error", err)
		}
	}
}
