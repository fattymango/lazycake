package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/pricing"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/id"
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
	s.reclaimOverdue(ctx)

	nodes, err := s.Store.ListNodes(ctx)
	if err != nil {
		s.Log.Error("listing nodes for placement", "error", err)
		return
	}
	for _, n := range nodes {
		if !n.Connected {
			continue
		}
		// "Below 0.2, stop dispatching and freeze the balance" (task 5.3).
		if s.Trust.Banned(n.ID) {
			continue
		}
		if s.maybeDispatchCanary(ctx, n) {
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
		return s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
	}

	// Fleet-wide cold-pull cap (task 5.4): if this node doesn't already
	// have the image and the cap is already held by maxConcurrentColdPulls
	// other nodes, put the task back rather than start a 4th concurrent
	// pull - it'll be picked up next tick, either by one of those nodes
	// once it's warm or by a different, already-warm node.
	digest := imageDigest(task.Image)
	warm, err := s.nodeHasImage(ctx, n.ID, digest)
	if err != nil {
		s.Log.Warn("checking image cache, proceeding as cold", "node_id", n.ID, "digest", digest, "error", err)
	}
	if !warm && !s.ColdPull.TryStart(digest, n.ID) {
		s.Log.Info("deferring dispatch, fleet-wide cold-pull cap reached", "node_id", n.ID, "task_id", task.ID, "digest", digest)
		return s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
	}

	// Deduct a hold at dispatch (task 4.5), before the task is actually
	// sent anywhere; a retried task keeps the hold from its first dispatch
	// (ErrDuplicate here just means "already held," not a problem - the
	// worst case doesn't change between attempts of the same task),
	// released whichever way the task eventually finishes
	// (billing.Ledger.Settle).
	worstCase := pricing.WorstCase(s.rates(), task.Limits)
	if err := s.Store.PlaceHold(ctx, task.ID, task.AccountID, worstCase); err != nil && err != store.ErrDuplicate {
		s.Log.Warn("placing balance hold, requeueing", "node_id", n.ID, "task_id", task.ID, "error", err)
		if !warm {
			s.ColdPull.Finish(digest, n.ID)
		}
		return s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
	}

	msg, err := s.dispatchMessage(ctx, task, requeueAfter.Add(-leaseMargin))
	if err != nil {
		s.Log.Warn("building dispatch message, requeueing", "node_id", n.ID, "task_id", task.ID, "error", err)
		if !warm {
			s.ColdPull.Finish(digest, n.ID)
		}
		return s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
	}
	if err := s.Dispatch.Send(n.ID, msg); err != nil {
		s.Log.Warn("dispatch send failed, requeueing", "node_id", n.ID, "task_id", task.ID, "error", err)
		if !warm {
			s.ColdPull.Finish(digest, n.ID)
		}
		// Self-healing: a send failure here means the registry already
		// doesn't have this node (see api.Registry.Send's "node not
		// connected" error), so nodes.connected is stale - most often
		// because the coordinator's own process was killed before its
		// Connect handler's disconnect-cleanup defer got to run (see
		// api.Server.Connect), not anything the node did wrong. Left
		// uncorrected, this node stays "connected" in the DB forever and
		// every future tick tries and fails to dispatch to it again
		// before any real node gets a turn - a genuine starvation bug,
		// caught live: a task bounced between claim and revert against a
		// long-dead node indefinitely, never reaching a healthy one.
		if err := s.Store.SetNodeConnected(ctx, n.ID, false); err != nil {
			s.Log.Warn("marking stale node disconnected", "node_id", n.ID, "error", err)
		}
		return s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
	}

	if err := s.Store.TransitionTask(ctx, task.ID, []store.TaskState{store.TaskReserved}, store.TaskDispatched, store.TaskUpdate{}); err != nil {
		return fmt.Errorf("marking task dispatched: %w", err)
	}
	s.Log.Info("task dispatched", "task_id", task.ID, "node_id", n.ID, "image", task.Image)
	s.Bus.Publish(events.Event{Type: "task_state", AtMS: s.now().UnixMilli(), AccountID: task.AccountID, TaskID: task.ID, NodeID: n.ID, State: string(store.TaskDispatched)})
	return nil
}

// maybeDispatchCanary implements task 5.2's injection half: with
// probability canaryRate(trust score), send this node a canary instead of
// a real queued task, indistinguishable from one in the Dispatch message
// itself - it's a normal store.Task, built and sent through the exact same
// dispatchMessage path as anything else. Returns true if a canary was
// (attempted to be) sent, so the caller skips a real dispatch this tick.
func (s *Scheduler) maybeDispatchCanary(ctx context.Context, n store.Node) bool {
	if s.PlatformGatewayID == "" || s.PlatformAccountID == "" || s.CanaryImage == "" {
		return false // not configured - no platform account/gateway/workload to canary against
	}
	if s.rand() >= canaryRate(s.Trust.Score(n.ID)) {
		return false
	}

	task := store.Task{
		ID: id.New(id.Task), AccountID: s.PlatformAccountID, State: store.TaskQueued,
		Image: s.CanaryImage, Entrypoint: s.CanaryEntrypoint, Args: s.CanaryArgs,
		Limits: store.Limits{
			CPUCores: 0.1, MemoryMB: 64, DiskMB: 100,
			WallTimeoutS: s.CanaryExpectedRuntimeS + 10,
		},
		Requirements: store.Requirements{Arch: n.Arch, Isolation: "podman"},
		TunnelTargets: []store.TunnelTarget{
			{GatewayID: s.PlatformGatewayID, Hostname: s.CanaryTargetHostname, Port: s.CanaryTargetPort},
		},
		GatewayIDs: []string{s.PlatformGatewayID},
		Delivery:   store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
	}
	if err := s.Store.CreateTask(ctx, task); err != nil {
		s.Log.Warn("creating canary task", "node_id", n.ID, "error", err)
		return false
	}
	if err := s.Store.TransitionTask(ctx, task.ID, []store.TaskState{store.TaskQueued}, store.TaskReserved,
		store.TaskUpdate{NodeID: &n.ID}); err != nil {
		s.Log.Warn("reserving canary task", "node_id", n.ID, "task_id", task.ID, "error", err)
		return false
	}

	leaseExpires := s.now().Add(time.Duration(s.LeaseS)*time.Second + leaseMargin)
	msg, err := s.dispatchMessage(ctx, task, leaseExpires)
	if err != nil {
		s.Log.Warn("building canary dispatch message", "node_id", n.ID, "task_id", task.ID, "error", err)
		return false
	}
	if err := s.Dispatch.Send(n.ID, msg); err != nil {
		s.Log.Warn("sending canary dispatch", "node_id", n.ID, "task_id", task.ID, "error", err)
		return false
	}
	if err := s.Store.TransitionTask(ctx, task.ID, []store.TaskState{store.TaskReserved}, store.TaskDispatched, store.TaskUpdate{}); err != nil {
		s.Log.Warn("marking canary dispatched", "node_id", n.ID, "task_id", task.ID, "error", err)
		return false
	}

	s.Canary.Expect(task.ID, n.ID, s.CanaryExpectedRuntimeS)
	s.Log.Info("canary dispatched", "node_id", n.ID, "task_id", task.ID)
	return true
}

// imageDigest extracts the "sha256:..." portion of a digest-pinned image
// reference ("repo@sha256:..."), matching how CustomerServer.SubmitTask
// already validates every task's image is pinned in the first place.
func imageDigest(image string) string {
	for i := len(image) - 1; i >= 0; i-- {
		if image[i] == '@' {
			return image[i+1:]
		}
	}
	return image
}

func (s *Scheduler) nodeHasImage(ctx context.Context, nodeID, digest string) (bool, error) {
	nodes, err := s.Store.NodesWithImage(ctx, digest)
	if err != nil {
		return false, err
	}
	for _, id := range nodes {
		if id == nodeID {
			return true, nil
		}
	}
	return false, nil
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

func (s *Scheduler) dispatchMessage(ctx context.Context, t store.Task, leaseExpires time.Time) (*lazycakev1.CoordinatorMessage, error) {
	targets := make([]*lazycakev1.TunnelTarget, len(t.TunnelTargets))
	for i, tt := range t.TunnelTargets {
		gw, err := s.Store.GetGateway(ctx, tt.GatewayID)
		if err != nil {
			return nil, fmt.Errorf("looking up gateway %s for target %s: %w", tt.GatewayID, tt.Hostname, err)
		}
		if len(gw.NoisePubkey) == 0 {
			return nil, fmt.Errorf("gateway %s has not published a noise key yet (has it ever connected?)", tt.GatewayID)
		}
		targets[i] = &lazycakev1.TunnelTarget{
			GatewayId: tt.GatewayID, Hostname: tt.Hostname, Port: tt.Port, NoisePubkey: gw.NoisePubkey,
		}
	}

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
				Targets:            targets,
				LeaseExpiresUnixMs: leaseExpires.UnixMilli(),
				Attempt:            int32(t.Attempt),
			},
		},
	}, nil
}
