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

	// A fresh keypair per run is a phase 2.4 simplification: real key
	// persistence and publication happens at gateway registration (phase
	// 2.7). Logged so it can be copied into the coordinator's gateway
	// record by hand until that exists.
	keypair, err := noise.GenerateKeypair()
	if err != nil {
		return fmt.Errorf("generating noise keypair: %w", err)
	}
	log.Info("noise static public key", "pubkey_hex", fmt.Sprintf("%x", keypair.Public))

	services := make(map[string]int, len(cfg.Services))
	for _, s := range cfg.Services {
		services[s.Name] = s.Port
	}

	conn, err := quic.DialGateway(ctx, cfg.CoordinatorAddr, cfg.Token, gatewayIDFromToken(cfg.Token))
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

// gatewayIDFromToken is a phase 2.4 stand-in for real gateway identity:
// until registration (phase 2.7) assigns a gw_<ulid> ID stored alongside
// the token, the gateway just is its own token for relay lookup purposes.
func gatewayIDFromToken(token string) string {
	return token
}
