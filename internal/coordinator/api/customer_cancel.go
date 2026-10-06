package api

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// stopReason is the exit reason recorded when a customer stops their own
// task. Unlike "cancelled" (the coordinator moved on; never billed) it is
// billable: see billing.billable.
const stopReason = "stopped"

func isFinishedState(st store.TaskState) bool {
	switch st {
	case store.TaskSucceeded, store.TaskFailed, store.TaskFenced, store.TaskAbandoned, store.TaskCancelled:
		return true
	}
	return false
}

// CancelTaskForAccount stops accountID's task at their request.
//
//   - queued: it never ran, so it is cancelled on the spot and nothing is
//     charged.
//   - reserved/dispatched/running: the request is recorded and the node is told
//     to stop it. The task isn't finished yet; the node reports back and the
//     normal finish path settles it (charging only the time it ran). The
//     scheduler keeps repeating the stop until that happens (a node still
//     pulling the image can't act on it yet), and finishes the task itself if
//     the node has vanished.
//   - already finished: an error, except that stopping a task that was already
//     stopped is a harmless repeat and succeeds.
//
// Everything races with the task finishing on its own, so each step is
// conditional on the state it read and the whole thing re-reads on conflict.
func (s *CustomerServer) CancelTaskForAccount(ctx context.Context, accountID, taskID string) (store.Task, error) {
	var task store.Task
	for attempt := 0; attempt < 4; attempt++ {
		var err error
		task, err = s.Store.GetTask(ctx, taskID)
		if err != nil {
			if err == store.ErrNotFound {
				return store.Task{}, status.Error(codes.NotFound, "task not found")
			}
			return store.Task{}, status.Errorf(codes.Internal, "getting task: %v", err)
		}
		if task.AccountID != accountID {
			return store.Task{}, status.Error(codes.NotFound, "task not found")
		}

		if isFinishedState(task.State) {
			if task.State == store.TaskCancelled && task.ExitReason != nil && *task.ExitReason == stopReason {
				return task, nil // already stopped: a repeat is not an error
			}
			return task, status.Error(codes.FailedPrecondition, "this task has already finished")
		}

		if task.State == store.TaskQueued {
			now := time.Now()
			code, reason := -1, stopReason
			err := s.Store.TransitionTask(ctx, taskID, []store.TaskState{store.TaskQueued}, store.TaskCancelled,
				store.TaskUpdate{ExitCode: &code, ExitReason: &reason, FinishedAt: &now})
			if err == store.ErrConflict {
				continue // a node claimed it a moment ago: take the "active" path on the next pass
			}
			if err != nil {
				return store.Task{}, status.Errorf(codes.Internal, "cancelling task: %v", err)
			}
			_ = s.Store.ReleaseHold(ctx, taskID) // a queued task normally has none; harmless if so
			s.publishState(accountID, taskID, "", store.TaskCancelled)
			return s.reload(ctx, taskID)
		}

		// reserved, dispatched or running.
		active, err := s.Store.RequestTaskCancel(ctx, taskID)
		if err != nil {
			return store.Task{}, status.Errorf(codes.Internal, "requesting cancel: %v", err)
		}
		if !active {
			continue // it just finished; the next pass reports that
		}
		nodeID := ""
		if task.NodeID != nil {
			nodeID = *task.NodeID
			s.sendStop(nodeID, taskID) // don't wait for the scheduler's next tick
		}
		s.publishState(accountID, taskID, nodeID, task.State) // state unchanged: tells the UI to show "stopping"
		return s.reload(ctx, taskID)
	}
	return task, status.Error(codes.Aborted, "the task kept changing state while stopping it; try again")
}

func (s *CustomerServer) reload(ctx context.Context, taskID string) (store.Task, error) {
	t, err := s.Store.GetTask(ctx, taskID)
	if err != nil {
		return store.Task{}, status.Errorf(codes.Internal, "reloading task: %v", err)
	}
	return t, nil
}

func (s *CustomerServer) sendStop(nodeID, taskID string) {
	if s.Registry == nil {
		return
	}
	// Best effort: if the node isn't reachable right now the scheduler keeps
	// trying, and finishes the task itself if the node never comes back.
	_ = s.Registry.Send(nodeID, &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_Cancel{
		Cancel: &lazycakev1.Cancel{TaskId: taskID, Reason: "stopped by customer"},
	}})
}

func (s *CustomerServer) publishState(accountID, taskID, nodeID string, st store.TaskState) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{Type: "task_state", AtMS: time.Now().UnixMilli(), AccountID: accountID, TaskID: taskID, NodeID: nodeID, State: string(st)})
}

// CancelTask is the gRPC form of CancelTaskForAccount (lcctl cancel).
func (s *CustomerServer) CancelTask(ctx context.Context, req *lazycakev1.CancelTaskRequest) (*lazycakev1.TaskStatus, error) {
	tok, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	task, err := s.CancelTaskForAccount(ctx, tok.AccountID, req.GetTaskId())
	if err != nil {
		return nil, err
	}
	return taskStatusProto(task), nil
}
