package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/mkassab215/lazycake/internal/agent/bench"
	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/config"
	"github.com/mkassab215/lazycake/internal/agent/conn"
	lcexec "github.com/mkassab215/lazycake/internal/agent/exec"
	"github.com/mkassab215/lazycake/internal/agent/lease"
	"github.com/mkassab215/lazycake/internal/agent/probe"
	"github.com/mkassab215/lazycake/internal/agent/reconcile"
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

	// Kill anything left behind by a previous run before accepting any new
	// work (IMPLEMENTATION.md task 3.7, PLAN.md "Killing orphans") - this
	// must happen before runner.Run below, since that's what starts
	// dispatching.
	if err := reconcile.Sweep(ctx, rt, instanceID, bootID, log); err != nil {
		return fmt.Errorf("startup reconciliation sweep: %w", err)
	}

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

	lcinitPath := cfg.LcinitPath
	if lcinitPath == "" {
		lcinitPath = discoverLcinit(log)
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
		LcinitPath:   lcinitPath,
	}
	benchTrigger := make(chan struct{}, 1)
	runner.Handlers = conn.Handlers{
		OnDispatch:     executor.HandleDispatch,
		OnCancel:       func(ctx context.Context, c *lazycakev1.Cancel) { executor.CancelTask(ctx, c.GetTaskId()) },
		RunningTaskIDs: ledger.TaskIDs,
		OnRegistered: func(ack *lazycakev1.RegisterAck) {
			executor.ReplayPending()
			if ack.GetBenchmarkNow() {
				select {
				case benchTrigger <- struct{}{}:
				default:
				}
			}
		},
	}
	go runBenchLoop(ctx, ledger, runner, log, benchTrigger)

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

// benchPeriod is how often the benchmark reruns on its own (task 4.1:
// "every 24 hours"), independent of the coordinator ever asking for one.
const benchPeriod = 24 * time.Hour

// runBenchLoop drives internal/agent/bench on the cadence task 4.1 asks
// for: once triggered (a fresh registration where the coordinator's
// RegisterAck says BenchmarkNow, i.e. it has no score on file for this
// node yet) or every benchPeriod after that, but never while any task is
// actually running - a benchmark racing against a task's own CPU use
// would just measure contention, not the host's real speed.
func runBenchLoop(ctx context.Context, ledger *capacity.Ledger, sender lcexec.Sender, log *slog.Logger, trigger <-chan struct{}) {
	ticker := time.NewTicker(time.Hour) // just how often to check whether benchPeriod has elapsed
	defer ticker.Stop()
	var last time.Time

	runIfIdle := func() {
		if len(ledger.TaskIDs()) > 0 {
			log.Info("skipping scheduled benchmark, tasks are currently running")
			return
		}
		score := bench.Run()
		last = time.Now()
		log.Info("benchmark complete", "score", score)
		sender.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_BenchReport{
			BenchReport: &lazycakev1.BenchReport{Score: score},
		}})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-trigger:
			runIfIdle()
		case <-ticker.C:
			if last.IsZero() || time.Since(last) >= benchPeriod {
				runIfIdle()
			}
		}
	}
}

// discoverLcinit looks for a "lcinit" binary next to the agent's own
// executable, so a normal side-by-side install (both binaries dropped in
// the same bin/ directory, as deploy/Dockerfile and `make build` both do)
// works with zero configuration. Returns "" (wrapper disabled, task 3.6's
// deadline enforcement falls back to the agent-side context deadline and
// task 3.5's systemd-slice cleanup) rather than failing startup - lcinit is
// defense in depth, not a hard requirement to run at all.
func discoverLcinit(log *slog.Logger) string {
	self, err := os.Executable()
	if err != nil {
		log.Warn("lcinit: could not determine own executable path, wrapper disabled", "error", err)
		return ""
	}
	candidate := filepath.Join(filepath.Dir(self), "lcinit")
	if _, err := os.Stat(candidate); err != nil {
		log.Warn("lcinit: no binary found next to the agent executable, wrapper disabled", "looked_at", candidate)
		return ""
	}
	return candidate
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
