package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mkassab215/lazycake/internal/agent/config"
	"github.com/mkassab215/lazycake/internal/agent/conn"
	"github.com/mkassab215/lazycake/internal/id"
	"github.com/mkassab215/lazycake/internal/logging"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
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

	clientConn, err := grpc.NewClient(cfg.CoordinatorAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer clientConn.Close()

	hostname, _ := os.Hostname()
	runner := &conn.Runner{
		Client: lazycakev1.NewAgentServiceClient(clientConn),
		Identity: conn.Identity{
			Token:      cfg.Token,
			Hostname:   hostname,
			Arch:       runtime.GOARCH,
			InstanceID: id.New("ins"),
			Offer: &lazycakev1.Offer{
				Cores:    cfg.OfferCores,
				MemoryMb: int32(cfg.OfferMemoryMB),
				DiskMb:   int32(cfg.OfferDiskMB),
			},
		},
		Log: log,
	}

	err = runner.Run(ctx)
	log.Info("agent shutting down")
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// runProbe implements `agent probe`; filled in with real capability checks
// in phase 1 task 1.5.
func runProbe() error {
	log := logging.New(os.Stderr, true)
	log.Warn("probe not implemented yet")
	return nil
}
