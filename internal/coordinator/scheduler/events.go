package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

func (s *Scheduler) OnTaskAccepted(ctx context.Context, nodeID, taskID string) error {
	s.Log.Info("task accepted", "task_id", taskID, "node_id", nodeID)
	return nil
}

func (s *Scheduler) OnTaskRejected(ctx context.Context, nodeID, taskID, reason string) error {
	s.Log.Warn("task rejected", "task_id", taskID, "node_id", nodeID, "reason", reason)
	s.mu.Lock()
	s.rejected[rejectKey{nodeID, taskID}] = s.now().Add(rejectCooldown)
	s.mu.Unlock()

	err := s.Store.TransitionTask(ctx, taskID,
		[]store.TaskState{store.TaskReserved, store.TaskDispatched}, store.TaskQueued, store.TaskUpdate{})
	if err != nil && err != store.ErrConflict {
		return fmt.Errorf("requeueing rejected task: %w", err)
	}
	return nil
}

func (s *Scheduler) OnTaskStarted(ctx context.Context, nodeID, taskID string, at time.Time) error {
	err := s.Store.TransitionTask(ctx, taskID,
		[]store.TaskState{store.TaskDispatched}, store.TaskRunning, store.TaskUpdate{StartedAt: &at})
	if err != nil && err != store.ErrConflict {
		return fmt.Errorf("marking task running: %w", err)
	}
	s.Log.Info("task started", "task_id", taskID, "node_id", nodeID)
	if err := s.billing().OnTaskStarted(ctx, nodeID, taskID); err != nil {
		s.Log.Error("metering task started", "task_id", taskID, "node_id", nodeID, "error", err)
	}
	return nil
}

func (s *Scheduler) OnTaskFinished(ctx context.Context, nodeID string, ev api.TaskFinishedEvent) error {
	to := store.TaskFailed
	if ev.ExitReason == "exited" && ev.ExitCode == 0 {
		to = store.TaskSucceeded
	}
	exitCode := int(ev.ExitCode)
	exitReason := ev.ExitReason
	err := s.Store.TransitionTask(ctx, ev.TaskID,
		[]store.TaskState{store.TaskDispatched, store.TaskRunning}, to,
		store.TaskUpdate{ExitCode: &exitCode, ExitReason: &exitReason, FinishedAt: &ev.At})
	if err != nil && err != store.ErrConflict {
		return fmt.Errorf("marking task finished: %w", err)
	}
	s.Log.Info("task finished", "task_id", ev.TaskID, "node_id", nodeID, "exit_code", ev.ExitCode, "exit_reason", ev.ExitReason)
	if err := s.billing().OnTaskFinished(ctx, nodeID, ev.TaskID, ev.BytesSent+ev.BytesRecv); err != nil {
		s.Log.Error("metering task finished", "task_id", ev.TaskID, "node_id", nodeID, "error", err)
	}
	return nil
}

func (s *Scheduler) OnCapacityReport(ctx context.Context, nodeID string, rep api.CapacityReportEvent) error {
	s.mu.Lock()
	s.freeCap[nodeID] = store.CapacityFilter{
		FreeCores:    rep.FreeCores,
		FreeMemoryMB: int(rep.FreeMemoryMB),
		FreeDiskMB:   int(rep.FreeDiskMB),
	}
	s.mu.Unlock()
	return nil
}
