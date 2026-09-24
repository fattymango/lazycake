package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/fattymango/lazycake/internal/coordinator/config"
	"github.com/fattymango/lazycake/internal/logging"
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

	<-ctx.Done()
	log.Info("coordinator shutting down")
	return nil
}
