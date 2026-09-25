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
	// A rejected task never pulled anything on this node (task 5.4): the
	// agent's own admission control - a separate check from the
	// coordinator's optimistic capacity filter - refused it before Pull
	// ever ran.
	if task, err := s.Store.GetTask(ctx, taskID); err == nil {
		s.ColdPull.Finish(imageDigest(task.Image), nodeID)
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

	// Started can only arrive after the agent's own Pull->Create->Start
	// sequence has already pulled the image successfully (task 5.4) - this
	// is the earliest correct point to release a cold-pull slot.
	if task, err := s.Store.GetTask(ctx, taskID); err == nil {
		s.ColdPull.Finish(imageDigest(task.Image), nodeID)
	}
	return nil
}

func (s *Scheduler) OnTaskFinished(ctx context.Context, nodeID string, ev api.TaskFinishedEvent) error {
	to := terminalState(ev.ExitReason, ev.ExitCode)
	exitCode := int(ev.ExitCode)
	exitReason := ev.ExitReason
	err := s.Store.TransitionTask(ctx, ev.TaskID,
		[]store.TaskState{store.TaskDispatched, store.TaskRunning}, to,
		store.TaskUpdate{ExitCode: &exitCode, ExitReason: &exitReason, FinishedAt: &ev.At})
	if err != nil && err != store.ErrConflict {
		return fmt.Errorf("marking task finished: %w", err)
	}
	s.Log.Info("task finished", "task_id", ev.TaskID, "node_id", nodeID, "exit_code", ev.ExitCode, "exit_reason", ev.ExitReason, "state", to)

	// Safety net for a cold-pull slot never released at OnTaskStarted -
	// e.g. the pull itself failed, so Started was never sent at all
	// (task 5.4). A no-op if OnTaskStarted already released it.
	if task, err := s.Store.GetTask(ctx, ev.TaskID); err == nil {
		s.ColdPull.Finish(imageDigest(task.Image), nodeID)
	}

	if err := s.billing().OnTaskFinished(ctx, nodeID, ev); err != nil {
		s.Log.Error("metering task finished", "task_id", ev.TaskID, "node_id", nodeID, "error", err)
	}

	// Clean completion / spec drift (tasks 5.1, 5.3): the host actually
	// ran the task and reported honestly, whatever the customer's own
	// exit code was - same classification billing.billable() uses, since
	// it's the same underlying question ("did this host do real work").
	if to == store.TaskSucceeded || to == store.TaskFailed {
		s.Trust.CleanCompletion(nodeID)

		// normalised_s was just persisted by billing above - read it back
		// rather than restructuring BillingEvents to hand it over
		// directly, since spec drift is a scheduler-owned concern billing
		// has no reason to know about.
		if meter, err := s.Store.GetMeter(ctx, ev.TaskID); err == nil && meter.NormalisedS != nil {
			ratio, flagged := s.SpecDrift.Observe(nodeID, *meter.NormalisedS)
			if flagged {
				s.Log.Warn("spec drift detected", "node_id", nodeID, "task_id", ev.TaskID, "rolling_ratio", ratio)
				s.Trust.SpecDrift(nodeID)
			}
		}
	}
	return nil
}

// terminalState maps a TaskFinished report to the task's terminal state -
// distinguishing an honest fence (task 3.2) and a coordinator-issued
// cancellation (task 3.3) from an ordinary exit matters for billing (task
// 4.4, PLAN.md "Payout rules": a fenced task pays no compute but must be
// recorded distinctly, not silently merged into "failed" the way a
// customer's own nonzero exit is).
func terminalState(exitReason string, exitCode int32) store.TaskState {
	switch exitReason {
	case "fenced":
		return store.TaskFenced
	case "cancelled":
		return store.TaskCancelled
	case "exited":
		if exitCode == 0 {
			return store.TaskSucceeded
		}
		return store.TaskFailed
	default: // "error", "wall_timeout", "oom", "egress_exceeded"
		return store.TaskFailed
	}
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
