package scheduler

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/fattymango/lazycake/internal/coordinator/events"
	"github.com/fattymango/lazycake/internal/coordinator/pricing"
	"github.com/fattymango/lazycake/internal/coordinator/store"
	"github.com/fattymango/lazycake/internal/id"
	lazycakev1 "github.com/fattymango/lazycake/internal/proto/lazycake/v1"
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
	s.resendCancels(ctx)

	nodes, err := s.Store.ListNodes(ctx)
	if err != nil {
		s.Log.Error("listing nodes for placement", "error", err)
		return
	}
	now := s.now()
	var elapsed time.Duration
	if !s.lastTick.IsZero() {
		elapsed = now.Sub(s.lastTick)
	}
	s.lastTick = now
	for _, n := range nodes {
		if !n.Connected {
			continue
		}
		// A connected node's trust heals with time, so a ban is never forever.
		s.Trust.Heal(n.ID, elapsed)
		// "Below 0.2, stop dispatching and freeze the balance" (task 5.3). Said once per ban, because a
		// silent skip looked exactly like a broken scheduler: 600 queued tasks and nothing in the logs.
		if s.Trust.Banned(n.ID) {
			if s.noteBan(n.ID, true) {
				s.Log.Warn("not dispatching to this node: its trust score is below the ban threshold; it heals with time",
					"node_id", n.ID, "trust", s.Trust.Score(n.ID), "threshold", TrustBanThreshold)
			}
			continue
		}
		if s.noteBan(n.ID, false) {
			s.Log.Info("node trust is back above the ban threshold; dispatching again", "node_id", n.ID, "trust", s.Trust.Score(n.ID))
		}
		if s.maybeDispatchCanary(ctx, n) {
			continue
		}
		s.fillNode(ctx, n)
	}
}

// noteBan records whether nodeID is currently banned and reports whether that just changed.
func (s *Scheduler) noteBan(nodeID string, banned bool) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.banned == nil {
		s.banned = make(map[string]bool)
	}
	if s.banned[nodeID] == banned {
		return false
	}
	s.banned[nodeID] = banned
	return true
}

// maxPlacementsPerTick bounds how many tasks one node is given in a single tick. A node that has just freed many cores (a batch of short
// tasks ending together) was refilled at one task a second; now it is refilled as fast as tasks fit, within this bound.
const maxPlacementsPerTick = 16

// fillNode places queued tasks on one node until none fits or the per-tick bound is reached. Each dispatch is subtracted from the node's
// free capacity as it goes (see reserveFree), so this never offers the node more than it has.
func (s *Scheduler) fillNode(ctx context.Context, n store.Node) {
	for i := 0; i < maxPlacementsPerTick; i++ {
		placed, err := s.tryPlaceOne(ctx, n)
		if err != nil && err != store.ErrNoTask {
			s.Log.Error("placing task", "node_id", n.ID, "error", err)
			return
		}
		if !placed {
			return
		}
	}
}

// reserveFree takes a dispatched task's limits off the node's free capacity right away. Until now the scheduler only learned a node's free
// capacity from the agent's next report, so it kept dispatching to a node it had just filled and the agent turned the work away ("does not fit
// free capacity"): about a quarter of dispatches in the production load test. The agent's next report overwrites this with the truth.
func (s *Scheduler) reserveFree(n store.Node, lim store.Limits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.freeCap[n.ID]
	if !ok {
		f = store.CapacityFilter{FreeCores: n.OfferCores, FreeMemoryMB: n.OfferMemoryMB, FreeDiskMB: n.OfferDiskMB}
	}
	f.FreeCores = math.Max(0, f.FreeCores-lim.CPUCores)
	f.FreeMemoryMB = max(0, f.FreeMemoryMB-lim.MemoryMB)
	f.FreeDiskMB = max(0, f.FreeDiskMB-lim.DiskMB)
	s.freeCap[n.ID] = f
}

// defaultStarveAfter is how long a task may wait before the nodes that could run it stop giving its cores to newer, smaller tasks.
const defaultStarveAfter = 90 * time.Second

func (s *Scheduler) starveAfter() time.Duration {
	if s.StarveAfter > 0 {
		return s.StarveAfter
	}
	return defaultStarveAfter
}

// holdForStarvingTask stops a node being filled with small newer tasks while an older, bigger one waits for room. Placement takes the oldest task
// that FITS the free cores, so a steady stream of small tasks keeps a node's free cores below what a big task needs and the big task is overtaken
// forever (in the production load test: 31 minutes' median wait for 4-core tasks and 42 for 8-core ones, against 5 to 9 for small ones; no 4-core task
// ever ran on the 4-core VM). If the oldest task this node could run when empty has waited longer than starveAfter and does not fit what is free
// now, the node may only place tasks older than it: its running work finishes, its cores free up, and the big task is claimed as soon as it fits.
// Nodes that could never run that task are unaffected.
func (s *Scheduler) holdForStarvingTask(ctx context.Context, n store.Node, filter *store.CapacityFilter) {
	offer := store.CapacityFilter{Arch: n.Arch, Isolations: filter.Isolations, FreeCores: n.OfferCores, FreeMemoryMB: n.OfferMemoryMB, FreeDiskMB: n.OfferDiskMB}
	head, err := s.Store.OldestQueuedBefore(ctx, offer, s.now().Add(-s.starveAfter()))
	if err != nil {
		if err != store.ErrNoTask {
			s.Log.Warn("looking for a waiting task", "node_id", n.ID, "error", err)
		}
		return
	}
	if head.Limits.CPUCores <= filter.FreeCores && head.Limits.MemoryMB <= filter.FreeMemoryMB && head.Limits.DiskMB <= filter.FreeDiskMB {
		return // it fits now: it is the oldest task that fits, so it is the one claimed next
	}
	// One node empties itself for a waiting task, not every node that could run it: the others keep working, or they would all sit idle
	// to make room for a task only one of them will run. The claim lapses if that node stops asking (disconnected, banned, task gone).
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.drainers[head.ID]; ok && d.nodeID != n.ID && now.Sub(d.at) < drainerLapse {
		return
	}
	for id, d := range s.drainers {
		if now.Sub(d.at) > time.Minute {
			delete(s.drainers, id)
		}
	}
	s.drainers[head.ID] = drainer{nodeID: n.ID, at: now}
	filter.OlderThan = head.CreatedAt
	filter.BackfillWallS = s.backfillWindow(ctx, n, head, *filter, now)
}

// backfillWindow is how many seconds of wall-clock limit a newer task may have and still be placed on a node that is draining for head: the time
// until the node would have room for head if every running task ran to its own wall limit, which is the latest it can happen. Such a task
// cannot delay head past that bound. 0 when it cannot be worked out, which keeps the plain hold.
func (s *Scheduler) backfillWindow(ctx context.Context, n store.Node, head store.Task, free store.CapacityFilter, now time.Time) int {
	running, err := s.Store.ListTasksByNode(ctx, n.ID, []store.TaskState{store.TaskRunning, store.TaskDispatched, store.TaskReserved})
	if err != nil || len(running) == 0 {
		return 0
	}
	type ends struct {
		at  time.Time
		lim store.Limits
	}
	list := make([]ends, 0, len(running))
	for _, t := range running {
		start := now // not started yet: it may start any moment, so count its whole limit from now
		if t.StartedAt != nil && t.StartedAt.Before(now) {
			start = *t.StartedAt
		}
		list = append(list, ends{start.Add(time.Duration(t.Limits.WallTimeoutS) * time.Second), t.Limits})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].at.Before(list[j].at) })
	cores, mem, disk := free.FreeCores, free.FreeMemoryMB, free.FreeDiskMB
	for _, e := range list {
		cores, mem, disk = cores+e.lim.CPUCores, mem+e.lim.MemoryMB, disk+e.lim.DiskMB
		if head.Limits.CPUCores <= cores && head.Limits.MemoryMB <= mem && head.Limits.DiskMB <= disk {
			return max(0, int(e.at.Sub(now).Seconds()))
		}
	}
	return 0
}

// drainerLapse is how long a node's claim on draining for a waiting task lasts without being renewed (it is renewed every tick while the node asks).
const drainerLapse = 5 * time.Second

// cooldownTasks are the tasks this node refused and is still cooling down from; they are skipped when claiming.
func (s *Scheduler) cooldownTasks(nodeID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var ids []string
	for k, until := range s.rejected {
		if k.nodeID == nodeID && now.Before(until) {
			ids = append(ids, k.taskID)
		}
	}
	return ids
}

// tryPlaceOne claims one queued task that fits the node and dispatches it. It reports whether a task was actually dispatched: a task that
// was put back (cooldown, cold-pull cap, hold or send failure) is not a placement, and the caller must not keep claiming after it.
func (s *Scheduler) tryPlaceOne(ctx context.Context, n store.Node) (bool, error) {
	filter := store.CapacityFilter{
		Arch:           n.Arch,
		Isolations:     isolationsFor(n),
		FreeCores:      s.freeCoresFor(n),
		FreeMemoryMB:   s.freeMemoryFor(n),
		FreeDiskMB:     s.freeDiskFor(n),
		ExcludeTaskIDs: s.cooldownTasks(n.ID),
	}
	s.holdForStarvingTask(ctx, n, &filter)

	requeueAfter := s.now().Add(time.Duration(s.LeaseS)*time.Second + leaseMargin)
	task, err := s.Store.ClaimQueuedTask(ctx, n.ID, filter, requeueAfter)
	if err != nil {
		return false, err
	}

	if s.inCooldown(n.ID, task.ID) {
		return false, s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
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
		return false, s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
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
		return false, s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
	}

	msg, err := s.dispatchMessage(ctx, task, requeueAfter.Add(-leaseMargin))
	if err != nil {
		s.Log.Warn("building dispatch message, requeueing", "node_id", n.ID, "task_id", task.ID, "error", err)
		if !warm {
			s.ColdPull.Finish(digest, n.ID)
		}
		return false, s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
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
		return false, s.Store.ReleaseReservedTask(ctx, task.ID, []store.TaskState{store.TaskReserved})
	}

	if err := s.Store.TransitionTask(ctx, task.ID, []store.TaskState{store.TaskReserved}, store.TaskDispatched, store.TaskUpdate{}); err != nil {
		return false, fmt.Errorf("marking task dispatched: %w", err)
	}
	s.Log.Info("task dispatched", "task_id", task.ID, "node_id", n.ID, "image", task.Image)
	s.Bus.Publish(events.Event{Type: "task_state", AtMS: s.now().UnixMilli(), AccountID: task.AccountID, TaskID: task.ID, NodeID: n.ID, State: string(store.TaskDispatched)})
	s.reserveFree(n, task.Limits)
	return true, nil
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
		if gw.ArchivedAt != nil {
			return nil, fmt.Errorf("gateway %s was deleted (target %s)", tt.GatewayID, tt.Hostname)
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
