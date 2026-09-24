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
	"time"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/runtime"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
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
}

// HandleDispatch is a conn.Handlers.OnDispatch-compatible callback: it
// returns immediately and runs the task in its own goroutine so it never
// blocks the connection's receive loop.
func (e *Executor) HandleDispatch(ctx context.Context, d *lazycakev1.Dispatch) {
	go e.run(ctx, d)
}

func (e *Executor) run(ctx context.Context, d *lazycakev1.Dispatch) {
	log := e.Log.With("task_id", d.GetTaskId())

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
		e.Send.Send(finished(d.GetTaskId(), -1, "error", time.Now()))
		return
	}

	id, err := e.Runtime.Create(runCtx, e.spec(d))
	if err != nil {
		log.Error("creating container", "error", err)
		e.Send.Send(finished(d.GetTaskId(), -1, "error", time.Now()))
		return
	}
	defer func() {
		if err := e.Runtime.Remove(context.Background(), id); err != nil {
			log.Warn("removing container", "container_id", id, "error", err)
		}
	}()

	if err := e.Runtime.Start(runCtx, id); err != nil {
		log.Error("starting container", "error", err)
		e.Send.Send(finished(d.GetTaskId(), -1, "error", time.Now()))
		return
	}
	e.Send.Send(started(d.GetTaskId(), time.Now()))

	go e.streamLogs(ctx, d.GetTaskId(), id)

	result, err := e.Runtime.Wait(runCtx, id)
	if err != nil {
		if runCtx.Err() != nil {
			log.Warn("task exceeded wall timeout, stopping", "wall_timeout_s", d.GetLimits().GetWallTimeoutS())
			_ = e.Runtime.Stop(context.Background(), id, 10*time.Second)
			e.Send.Send(finished(d.GetTaskId(), -1, "wall_timeout", time.Now()))
			return
		}
		log.Error("waiting for container", "error", err)
		e.Send.Send(finished(d.GetTaskId(), -1, "error", time.Now()))
		return
	}

	reason := "exited"
	if result.OOMKilled {
		reason = "oom"
	}
	log.Info("task finished", "exit_code", result.ExitCode, "exit_reason", reason)
	e.Send.Send(finished(d.GetTaskId(), int32(result.ExitCode), reason, time.Now()))
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
