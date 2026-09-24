// Package exec turns a coordinator Dispatch message into a running
// container and reports the result back: admission control, pull,
// create/start, wait, and TaskFinished. It depends on runtime.Runtime and
// capacity.Ledger only through their interfaces/exported types, and on the
// coordinator only through the wire messages in lazycakev1 - it never
// imports anything under internal/coordinator.
package exec

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/netns"
	"github.com/mkassab215/lazycake/internal/agent/runtime"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
	"github.com/mkassab215/lazycake/internal/tunnel/noise"
)

// Sender delivers one AgentMessage to the coordinator. *conn.Runner
// satisfies this structurally.
type Sender interface {
	Send(msg *lazycakev1.AgentMessage)
}

// Executor runs dispatched tasks against a Runtime under a capacity.Ledger.
type Executor struct {
	Runtime    runtime.Runtime
	Ledger     *capacity.Ledger
	Send       Sender
	Log        *slog.Logger
	InstanceID string
	BootID     string

	// RelayAddr, Token and AgentKeypair are only needed when a dispatched
	// task actually declares tunnel targets; a task with none never
	// touches internal/agent/netns at all.
	RelayAddr    string
	Token        string
	AgentKeypair noise.Keypair
}

// HandleDispatch is a conn.Handlers.OnDispatch-compatible callback: it
// returns immediately and runs the task in its own goroutine so it never
// blocks the connection's receive loop.
func (e *Executor) HandleDispatch(ctx context.Context, d *lazycakev1.Dispatch) {
	go e.run(ctx, d)
}

func (e *Executor) run(ctx context.Context, d *lazycakev1.Dispatch) {
	log := e.Log.With("task_id", d.GetTaskId())

	// A task may finish from several independent places (normal exit,
	// wall timeout, egress cap fired from a background goroutine) - Once
	// keeps the coordinator from seeing more than one TaskFinished for
	// the same task.
	var finishOnce sync.Once
	sendFinished := func(exitCode int32, reason string) {
		finishOnce.Do(func() { e.Send.Send(finished(d.GetTaskId(), exitCode, reason, time.Now())) })
	}

	want := capacity.Resources{
		Cores:    d.GetLimits().GetCpuCores(),
		MemoryMB: int(d.GetLimits().GetMemoryMb()),
		DiskMB:   int(d.GetLimits().GetDiskMb()),
	}
	if err := e.Ledger.Admit(d.GetTaskId(), want); err != nil {
		log.Warn("rejecting task, does not fit local capacity", "error", err)
		e.Send.Send(rejected(d.GetTaskId(), err.Error()))
		return
	}
	defer e.Ledger.Release(d.GetTaskId())

	e.Send.Send(accepted(d.GetTaskId()))

	wallTimeout := time.Duration(d.GetLimits().GetWallTimeoutS()) * time.Second
	runCtx := ctx
	var cancel context.CancelFunc
	if wallTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, wallTimeout)
		defer cancel()
	}

	if _, err := e.Runtime.Pull(runCtx, d.GetImage()); err != nil {
		log.Error("pulling image", "image", d.GetImage(), "error", err)
		sendFinished(-1, "error")
		return
	}

	id, err := e.Runtime.Create(runCtx, e.spec(d))
	if err != nil {
		log.Error("creating container", "error", err)
		sendFinished(-1, "error")
		return
	}
	defer func() {
		if err := e.Runtime.Remove(context.Background(), id); err != nil {
			log.Warn("removing container", "container_id", id, "error", err)
		}
	}()

	if err := e.Runtime.Start(runCtx, id); err != nil {
		log.Error("starting container", "error", err)
		sendFinished(-1, "error")
		return
	}
	e.Send.Send(started(d.GetTaskId(), time.Now()))

	go e.streamLogs(ctx, d.GetTaskId(), id)

	if len(d.GetTargets()) > 0 {
		proxy, err := e.startTunnel(ctx, id, d, sendFinished)
		if err != nil {
			// The container is already running with --network=none and no
			// route anywhere; failing to set up its one exception is the
			// same as the task simply being unable to reach its target, so
			// fail the task rather than let it run uselessly to timeout.
			log.Error("starting netns proxy", "error", err)
			_ = e.Runtime.Stop(context.Background(), id, 5*time.Second)
			sendFinished(-1, "error")
			return
		}
		defer proxy.Close()
	}

	result, err := e.Runtime.Wait(runCtx, id)
	if err != nil {
		if runCtx.Err() != nil {
			log.Warn("task exceeded wall timeout, stopping", "wall_timeout_s", d.GetLimits().GetWallTimeoutS())
			_ = e.Runtime.Stop(context.Background(), id, 10*time.Second)
			sendFinished(-1, "wall_timeout")
			return
		}
		log.Error("waiting for container", "error", err)
		sendFinished(-1, "error")
		return
	}

	reason := "exited"
	if result.OOMKilled {
		reason = "oom"
	}
	log.Info("task finished", "exit_code", result.ExitCode, "exit_reason", reason)
	sendFinished(int32(result.ExitCode), reason)
}

// startTunnel sets up internal/agent/netns for a dispatched task that
// declared tunnel targets: a --network=none container otherwise has no
// interface at all, so this is the one exception PLAN.md's design carves
// out, built inside the container's own network namespace.
func (e *Executor) startTunnel(ctx context.Context, containerID string, d *lazycakev1.Dispatch, sendFinished func(int32, string)) (*netns.Proxy, error) {
	pid, err := e.Runtime.Pid(ctx, containerID)
	if err != nil {
		return nil, err
	}

	targets := make([]netns.Target, len(d.GetTargets()))
	for i, t := range d.GetTargets() {
		targets[i] = netns.Target{
			GatewayID: t.GetGatewayId(), Hostname: t.GetHostname(), Port: t.GetPort(),
			NoisePubkey: t.GetNoisePubkey(),
		}
	}

	proxy := &netns.Proxy{
		ContainerPID: pid, ContainerID: containerID, TaskID: d.GetTaskId(),
		Targets: targets, AgentKeypair: e.AgentKeypair,
		RelayAddr: e.RelayAddr, Token: e.Token,
		EgressCapBytes: int64(d.GetLimits().GetEgressMb()) << 20,
		Runtime:        e.Runtime, Log: e.Log,
		OnEgressExceeded: func() {
			e.Log.Warn("egress cap exceeded, stopping task", "task_id", d.GetTaskId())
			_ = e.Runtime.Stop(context.Background(), containerID, 5*time.Second)
			sendFinished(-1, "egress_exceeded")
		},
	}
	if err := proxy.Setup(ctx); err != nil {
		return nil, err
	}
	return proxy, nil
}

func (e *Executor) spec(d *lazycakev1.Dispatch) runtime.Spec {
	return runtime.Spec{
		Name:       "lazycake-" + d.GetTaskId(),
		Image:      d.GetImage(),
		Entrypoint: d.GetEntrypoint(),
		Args:       d.GetArgs(),
		Env:        d.GetEnv(),
		Workdir:    d.GetWorkdir(),
		Isolation:  d.GetIsolation(),
		CPUCores:   d.GetLimits().GetCpuCores(),
		MemoryMB:   int(d.GetLimits().GetMemoryMb()),
		TmpfsMB:    int(d.GetLimits().GetTmpfsMb()),
		PIDs:       int(d.GetLimits().GetPids()),
		Labels: map[string]string{
			"lazycake.task_id":     d.GetTaskId(),
			"lazycake.instance_id": e.InstanceID,
			"lazycake.boot_id":     e.BootID,
		},
	}
}

func rejected(taskID, reason string) *lazycakev1.AgentMessage {
	return &lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Rejected{
		Rejected: &lazycakev1.TaskRejected{TaskId: taskID, Reason: reason},
	}}
}

func accepted(taskID string) *lazycakev1.AgentMessage {
	return &lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Accepted{
		Accepted: &lazycakev1.TaskAccepted{TaskId: taskID},
	}}
}

func started(taskID string, at time.Time) *lazycakev1.AgentMessage {
	return &lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Started{
		Started: &lazycakev1.TaskStarted{TaskId: taskID, AtUnixMs: at.UnixMilli()},
	}}
}

func finished(taskID string, exitCode int32, reason string, at time.Time) *lazycakev1.AgentMessage {
	return &lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Finished{
		Finished: &lazycakev1.TaskFinished{
			TaskId: taskID, ExitCode: exitCode, ExitReason: reason, AtUnixMs: at.UnixMilli(),
		},
	}}
}
