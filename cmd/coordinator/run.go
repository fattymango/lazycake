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
	"github.com/mkassab215/lazycake/internal/coordinator/config"
	"github.com/mkassab215/lazycake/internal/coordinator/scheduler"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/logging"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
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

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.GRPCAddr, err)
	}

	registry := api.NewRegistry()
	sched := scheduler.New(st, registry, clock.Real{}, log, leaseS)

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

	go sched.Run(ctx, time.Second)

	serveErr := make(chan error, 1)
	go func() { serveErr <- grpcServer.Serve(lis) }()
	log.Info("agent gRPC listening", "addr", cfg.GRPCAddr)

	select {
	case <-ctx.Done():
		log.Info("coordinator shutting down")
		grpcServer.GracefulStop()
		return nil
	case err := <-serveErr:
		return fmt.Errorf("grpc server: %w", err)
	}
}
