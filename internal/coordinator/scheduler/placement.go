package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// Run ticks the placement loop until ctx is cancelled: for each connected
// node, try to claim one matching queued task and dispatch it.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	nodes, err := s.Store.ListNodes(ctx)
	if err != nil {
		s.Log.Error("listing nodes for placement", "error", err)
		return
	}
	for _, n := range nodes {
		if !n.Connected {
			continue
		}
		if err := s.tryPlaceOne(ctx, n); err != nil && err != store.ErrNoTask {
			s.Log.Error("placing task", "node_id", n.ID, "error", err)
		}
	}
}

func (s *Scheduler) tryPlaceOne(ctx context.Context, n store.Node) error {
	filter := store.CapacityFilter{
		Arch:         n.Arch,
		Isolations:   isolationsFor(n),
		FreeCores:    s.freeCoresFor(n),
		FreeMemoryMB: s.freeMemoryFor(n),
		FreeDiskMB:   s.freeDiskFor(n),
	}

	requeueAfter := s.now().Add(time.Duration(s.LeaseS)*time.Second + leaseMargin)
	task, err := s.Store.ClaimQueuedTask(ctx, n.ID, filter, requeueAfter)
	if err != nil {
		return err
	}

	if s.inCooldown(n.ID, task.ID) {
		return s.Store.TransitionTask(ctx, task.ID, []store.TaskState{store.TaskReserved}, store.TaskQueued, store.TaskUpdate{})
	}

	msg := dispatchMessage(task, requeueAfter.Add(-leaseMargin))
	if err := s.Dispatch.Send(n.ID, msg); err != nil {
		s.Log.Warn("dispatch send failed, requeueing", "node_id", n.ID, "task_id", task.ID, "error", err)
		return s.Store.TransitionTask(ctx, task.ID, []store.TaskState{store.TaskReserved}, store.TaskQueued, store.TaskUpdate{})
	}

	if err := s.Store.TransitionTask(ctx, task.ID, []store.TaskState{store.TaskReserved}, store.TaskDispatched, store.TaskUpdate{}); err != nil {
		return fmt.Errorf("marking task dispatched: %w", err)
	}
	s.Log.Info("task dispatched", "task_id", task.ID, "node_id", n.ID, "image", task.Image)
	return nil
}

func isolationsFor(n store.Node) []string {
	isolations := []string{"podman"}
	if n.Capabilities.GVisor {
		isolations = append(isolations, "gvisor")
	}
	return isolations
}

// freeCoresFor/freeMemoryFor/freeDiskFor fall back to the node's full offer
// until its first CapacityReport arrives - the coordinator's view is
// allowed to be an optimistic overestimate since the agent's own capacity
// ledger is the final admission authority (PLAN.md "Agent and host").
func (s *Scheduler) freeCoresFor(n store.Node) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.freeCap[n.ID]; ok {
		return f.FreeCores
	}
	return n.OfferCores
}

func (s *Scheduler) freeMemoryFor(n store.Node) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.freeCap[n.ID]; ok {
		return f.FreeMemoryMB
	}
	return n.OfferMemoryMB
}

func (s *Scheduler) freeDiskFor(n store.Node) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.freeCap[n.ID]; ok {
		return f.FreeDiskMB
	}
	return n.OfferDiskMB
}

func (s *Scheduler) inCooldown(nodeID, taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.rejected[rejectKey{nodeID, taskID}]
	return ok && s.now().Before(until)
}

func dispatchMessage(t store.Task, leaseExpires time.Time) *lazycakev1.CoordinatorMessage {
	return &lazycakev1.CoordinatorMessage{
		Body: &lazycakev1.CoordinatorMessage_Dispatch{
			Dispatch: &lazycakev1.Dispatch{
				TaskId:     t.ID,
				Image:      t.Image,
				Entrypoint: t.Entrypoint,
				Args:       t.Args,
				Env:        t.Env,
				Workdir:    t.Workdir,
				Isolation:  t.Requirements.Isolation,
				Limits: &lazycakev1.Limits{
					CpuCores:         t.Limits.CPUCores,
					MemoryMb:         int32(t.Limits.MemoryMB),
					DiskMb:           int32(t.Limits.DiskMB),
					TmpfsMb:          int32(t.Limits.TmpfsMB),
					Pids:             int32(t.Limits.PIDs),
					WallTimeoutS:     int32(t.Limits.WallTimeoutS),
					NoOutputTimeoutS: int32(t.Limits.NoOutputTimeoutS),
					EgressMb:         int32(t.Limits.EgressMB),
				},
				LeaseExpiresUnixMs: leaseExpires.UnixMilli(),
			},
		},
	}
}
