package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mkassab215/lazycake/internal/gateway/config"
	"github.com/mkassab215/lazycake/internal/gateway/listener"
	"github.com/mkassab215/lazycake/internal/logging"
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

	services := make(map[string]int, len(cfg.Services))
	for _, s := range cfg.Services {
		services[s.Name] = s.Port
	}

	conn, err := quic.DialGateway(ctx, cfg.CoordinatorAddr, cfg.Token, cfg.GatewayID, keypair.Public)
	if err != nil {
		return fmt.Errorf("connecting to relay: %w", err)
	}

	l := &listener.Listener{
		Conn: conn, Keypair: keypair, Services: services, Log: log,
		OnForward: func(s listener.ForwardStats) {
			log.Info("forward closed", "task_id", s.TaskID, "service", s.Service,
				"bytes_to_local", s.BytesToLocal, "bytes_to_task", s.BytesToTask)
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
