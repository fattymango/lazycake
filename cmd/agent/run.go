package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/config"
	"github.com/mkassab215/lazycake/internal/agent/conn"
	lcexec "github.com/mkassab215/lazycake/internal/agent/exec"
	"github.com/mkassab215/lazycake/internal/agent/lease"
	"github.com/mkassab215/lazycake/internal/agent/probe"
	lcruntime "github.com/mkassab215/lazycake/internal/agent/runtime"
	"github.com/mkassab215/lazycake/internal/id"
	"github.com/mkassab215/lazycake/internal/logging"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
	"github.com/mkassab215/lazycake/internal/tunnel/noise"
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

	sock, err := lcruntime.DefaultSocket()
	if err != nil {
		return fmt.Errorf("finding container engine socket: %w", err)
	}
	rt, err := lcruntime.NewPodmanRuntime(sock)
	if err != nil {
		return fmt.Errorf("connecting to container engine: %w", err)
	}
	defer rt.Close()

	physical, err := capacity.Physical(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("reading physical host capacity: %w", err)
	}
	offer := capacity.ClampOffer(physical, capacity.Resources{
		Cores: cfg.OfferCores, MemoryMB: cfg.OfferMemoryMB, DiskMB: cfg.OfferDiskMB,
	})
	log.Info("offer clamped against physical capacity", "physical", physical, "requested", cfg.OfferCores, "clamped", offer)
	ledger := capacity.NewLedger(offer)

	clientConn, err := grpc.NewClient(cfg.CoordinatorAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer clientConn.Close()

	hostname, _ := os.Hostname()
	instanceID := id.New("ins")
	bootID := readBootID()

	runner := &conn.Runner{
		Client: lazycakev1.NewAgentServiceClient(clientConn),
		Identity: conn.Identity{
			Token:      cfg.Token,
			Hostname:   hostname,
			Arch:       runtime.GOARCH,
			InstanceID: instanceID,
			BootID:     bootID,
			Caps:       report.Capabilities("podman"),
			Offer: &lazycakev1.Offer{
				Cores:    offer.Cores,
				MemoryMb: int32(offer.MemoryMB),
				DiskMb:   int32(offer.DiskMB),
			},
		},
		Log: log,
	}

	tunnelKeypair, err := noise.GenerateKeypair()
	if err != nil {
		return fmt.Errorf("generating tunnel noise keypair: %w", err)
	}

	executor := &lcexec.Executor{
		Runtime:      rt,
		Ledger:       ledger,
		Send:         runner,
		Log:          log,
		InstanceID:   instanceID,
		BootID:       bootID,
		RelayAddr:    cfg.RelayAddr,
		Token:        cfg.Token,
		AgentKeypair: tunnelKeypair,
	}
	runner.Handlers = conn.Handlers{
		OnDispatch:     executor.HandleDispatch,
		RunningTaskIDs: ledger.TaskIDs,
		OnRegistered:   func(*lazycakev1.RegisterAck) { executor.ReplayPending() },
	}

	watcher := &lease.Watcher{
		Deadline: runner.FenceDeadline,
		Log:      log,
		OnFence: func() {
			fenceCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			executor.FenceAll(fenceCtx)
		},
	}
	go watcher.Run(ctx)

	err = runner.Run(ctx)
	log.Info("agent shutting down")
	if err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// readBootID returns the kernel's boot ID (used to label containers so the
// startup reconciliation sweep in phase 3.7 can tell "this run" apart from
// a stale one after a crash/reboot), or "" if it can't be read - e.g. off
// Linux, where the agent doesn't run in production anyway.
func readBootID() string {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
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
