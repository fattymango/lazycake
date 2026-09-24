package scheduler

import (
	"context"

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
			continue
		}
		if err := s.Store.AbandonTask(ctx, t.ID, []store.TaskState{t.State}, now); err != nil {
			if err != store.ErrConflict {
				s.Log.Error("abandoning overdue task", "task_id", t.ID, "error", err)
			}
			continue
		}
		s.Log.Warn("task abandoned after missed lease", "task_id", t.ID, "delivery", t.Delivery)
	}
}
