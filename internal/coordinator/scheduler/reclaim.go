package scheduler

import (
	"context"

	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// reclaimOverdue is IMPLEMENTATION.md task 3.4's reclaimer loop: any task
// whose requeue_after has passed while still reserved/dispatched/running
// has, per task 3.1's invariant, already been self-fenced by its agent (or
// the agent is gone) - the coordinator is never early. at_most_once tasks
// (the default) go straight to abandoned, since retrying one risks a
// double-write the customer never asked to tolerate (PLAN.md "Delivery
// defaults to at_most_once"). at_least_once tasks with attempts left go
// back to queued for a fresh claim by any connected node; once attempts
// are exhausted they're abandoned too, same as at_most_once.
func (s *Scheduler) reclaimOverdue(ctx context.Context) {
	now := s.now()
	tasks, err := s.Store.RequeueOverdue(ctx, now)
	if err != nil {
		s.Log.Error("listing overdue tasks", "error", err)
		return
	}
	for _, t := range tasks {
		// t.Attempt counts attempts already consumed before this failure;
		// this failed dispatch consumes one more, so retry only if that
		// still leaves at least one attempt under MaxAttempts.
		if t.Delivery == store.AtLeastOnce && t.Attempt+1 < t.Retry.MaxAttempts {
			if err := s.Store.RequeueTaskForRetry(ctx, t.ID, []store.TaskState{t.State}); err != nil {
				if err != store.ErrConflict {
					s.Log.Error("requeueing overdue task for retry", "task_id", t.ID, "error", err)
				}
				continue
			}
			s.Log.Warn("task requeued after missed lease", "task_id", t.ID, "attempt", t.Attempt+1)
			nodeID := ""
			if t.NodeID != nil {
				nodeID = *t.NodeID
			}
			s.Bus.Publish(events.Event{Type: "task_state", AtMS: now.UnixMilli(), TaskID: t.ID, NodeID: nodeID, State: string(store.TaskQueued)})
			continue
		}
		if err := s.Store.AbandonTask(ctx, t.ID, []store.TaskState{t.State}, now); err != nil {
			if err != store.ErrConflict {
				s.Log.Error("abandoning overdue task", "task_id", t.ID, "error", err)
			}
			continue
		}
		s.Log.Warn("task abandoned after missed lease", "task_id", t.ID, "delivery", t.Delivery)
		abandonedNodeID := ""
		if t.NodeID != nil {
			abandonedNodeID = *t.NodeID
		}
		s.Bus.Publish(events.Event{Type: "task_state", AtMS: now.UnixMilli(), TaskID: t.ID, NodeID: abandonedNodeID, State: string(store.TaskAbandoned)})

		// An abandoned task never reaches OnTaskFinished (the agent is
		// gone, there's no TaskFinished to receive), so nothing else ever
		// releases its dispatch-time hold (task 4.5) or drops the node's
		// trust (task 5.3, PLAN.md "a host that vanishes ... is paid
		// nothing") - both have to happen here instead.
		if err := s.Store.ReleaseHold(ctx, t.ID); err != nil {
			s.Log.Warn("releasing hold for abandoned task", "task_id", t.ID, "error", err)
		}
		if t.NodeID != nil {
			s.Trust.Abandoned(*t.NodeID)
			// Same safety net as OnTaskStarted/OnTaskFinished (task 5.4):
			// an abandoned task might have vanished mid-pull, never
			// reaching either.
			s.ColdPull.Finish(imageDigest(t.Image), *t.NodeID)
		}
	}
}
