package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mkassab215/lazycake/internal/agent/config"
	"github.com/mkassab215/lazycake/internal/agent/conn"
	"github.com/mkassab215/lazycake/internal/agent/probe"
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

	probeCtx, cancelProbe := context.WithTimeout(ctx, 30*time.Second)
	report, err := probe.Run(probeCtx, probe.DefaultOptions())
	cancelProbe()
	if err != nil {
		return fmt.Errorf("running capability probe: %w", err)
	}
	logProbeReport(log, report)
	if !report.OK() {
		return fmt.Errorf("capability probe failed: memory and/or cpu enforcement is not working; run 'agent probe' for details")
	}

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
			Caps:       report.Capabilities("podman"),
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

// runProbe implements `agent probe`: prints a table of every capability
// check and exits non-zero if memory or CPU enforcement failed.
func runProbe() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	report, err := probe.Run(ctx, probe.DefaultOptions())
	if err != nil {
		return fmt.Errorf("running probe: %w", err)
	}

	printProbeTable(report)

	if !report.OK() {
		return fmt.Errorf("memory and/or cpu enforcement is not working; this host cannot safely bill provisioned resources (see PLAN.md 'Preflight capability probe')")
	}
	return nil
}

func printProbeTable(report probe.Report) {
	for _, res := range report.Results {
		if res.Detail == "" {
			fmt.Printf("%-16s %s\n", res.Name, res.Status)
			continue
		}
		fmt.Printf("%-16s %s - %s\n", res.Name, res.Status, res.Detail)
	}
}

func logProbeReport(log *slog.Logger, report probe.Report) {
	for _, res := range report.Results {
		log.Info("capability probe", "check", res.Name, "status", res.Status, "detail", res.Detail)
	}
}
