package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/mkassab215/lazycake/internal/agent/config"
	"github.com/mkassab215/lazycake/internal/logging"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(nil)
	if err != nil {
		return err
	}

	log := logging.New(os.Stderr, cfg.Dev)
	log.Info("agent starting", "config", cfg)

	<-ctx.Done()
	log.Info("agent shutting down")
	return nil
}

func runProbe() error {
	cfg, _ := config.Load(func(string) string { return "" })
	log := logging.New(os.Stderr, cfg.Dev)
	log.Info("probe not implemented yet")
	return nil
}
