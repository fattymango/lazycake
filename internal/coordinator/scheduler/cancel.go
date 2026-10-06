package scheduler

import (
	"context"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// cancelResendEvery is how often a still-running task whose customer asked for
// it to be stopped gets another Cancel. The agent ignores a Cancel for a task
// it isn't running yet (the image is still being pulled) and one lost on a
// busy stream is simply never seen, so a single send isn't enough; repeating
// is harmless because the agent treats an unknown or finished task as a no-op.
const cancelResendEvery = 5 * time.Second

// resendCancels keeps nudging nodes to stop tasks the customer asked to stop,
// until they report back. Called every scheduler tick.
func (s *Scheduler) resendCancels(ctx context.Context) {
	tasks, err := s.Store.ListCancelRequested(ctx)
	if err != nil {
		s.Log.Error("listing tasks with a cancel request", "error", err)
		return
	}
	now := s.now()
	s.mu.Lock()
	if s.cancelSent == nil {
		s.cancelSent = make(map[string]time.Time)
	}
	live := make(map[string]struct{}, len(tasks))
	var due []store.Task
	for _, t := range tasks {
		live[t.ID] = struct{}{}
		if t.NodeID == nil {
			continue
		}
		if last, ok := s.cancelSent[t.ID]; ok && now.Sub(last) < cancelResendEvery {
			continue
		}
		s.cancelSent[t.ID] = now
		due = append(due, t)
	}
	for id := range s.cancelSent { // forget tasks that have finished
		if _, ok := live[id]; !ok {
			delete(s.cancelSent, id)
		}
	}
	s.mu.Unlock()

	for _, t := range due {
		msg := &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_Cancel{
			Cancel: &lazycakev1.Cancel{TaskId: t.ID, Reason: "stopped by customer"},
		}}
		if err := s.Dispatch.Send(*t.NodeID, msg); err != nil {
			// Node offline or its buffer full: the next tick tries again, and
			// if the node never comes back the reclaimer finishes the task.
			s.Log.Debug("could not send cancel yet", "task_id", t.ID, "node_id", *t.NodeID, "error", err)
		}
	}
}

// finishStopped ends a task that the customer asked to stop but whose node
// will never report back (it missed its lease). The task becomes cancelled
// with exit reason "stopped"; if it had started, the time it ran is billed
// through the same path as any other finish, so there is one place that
// settles money.
func (s *Scheduler) finishStopped(ctx context.Context, t store.Task, now time.Time) {
	exitCode := -1
	reason := "stopped"
	err := s.Store.TransitionTask(ctx, t.ID, []store.TaskState{t.State}, store.TaskCancelled,
		store.TaskUpdate{ExitCode: &exitCode, ExitReason: &reason, FinishedAt: &now})
	if err != nil {
		if err != store.ErrConflict { // conflict = it finished some other way meanwhile
			s.Log.Error("finishing a stopped task", "task_id", t.ID, "error", err)
		}
		return
	}
	nodeID := ""
	if t.NodeID != nil {
		nodeID = *t.NodeID
	}
	s.Log.Info("task stopped at the customer's request; its node is gone", "task_id", t.ID, "node_id", nodeID)
	s.Bus.Publish(events.Event{Type: "task_state", AtMS: now.UnixMilli(), AccountID: t.AccountID, TaskID: t.ID, NodeID: nodeID, State: string(store.TaskCancelled)})

	if t.StartedAt != nil && t.NodeID != nil {
		ev := api.TaskFinishedEvent{TaskID: t.ID, ExitCode: -1, ExitReason: "stopped", At: now}
		if err := s.billing().OnTaskFinished(ctx, nodeID, ev); err != nil {
			s.Log.Error("billing a stopped task", "task_id", t.ID, "error", err)
		}
	} else if err := s.Store.ReleaseHold(ctx, t.ID); err != nil {
		s.Log.Warn("releasing hold for a stopped task", "task_id", t.ID, "error", err)
	}
	if t.NodeID != nil {
		s.ColdPull.Finish(imageDigest(t.Image), *t.NodeID)
	}
}
