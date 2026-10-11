//go:build integration

package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fattymango/lazycake/internal/clock"
	"github.com/fattymango/lazycake/internal/coordinator/api"
	"github.com/fattymango/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/fattymango/lazycake/internal/proto/lazycake/v1"
)

// dispatchRecorder remembers which tasks were dispatched to which node.
type dispatchRecorder struct {
	mu    sync.Mutex
	tasks []string
}

func (d *dispatchRecorder) Send(_ string, msg *lazycakev1.CoordinatorMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if dp := msg.GetDispatch(); dp != nil {
		d.tasks = append(d.tasks, dp.GetTaskId())
	}
	return nil
}
func (d *dispatchRecorder) count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.tasks) }

func queuedTask(t *testing.T, st *store.PostgresStore, id string, cores float64, memMB int) {
	t.Helper()
	require.NoError(t, st.CreateTask(context.Background(), store.Task{
		ID: id, AccountID: "act_1", State: store.TaskQueued, Image: "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e",
		Entrypoint:   []string{"sh"},
		Limits:       store.Limits{CPUCores: cores, MemoryMB: memMB, DiskMB: 100, WallTimeoutS: 60},
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
	}))
}

// The scheduler used to give a node one task a tick and never subtract it from what it thought was free until the agent's next report, so it
// kept dispatching to a node it had just filled (the agent then rejected the work). It must now fill a node in one tick, stop exactly when the
// node is full, and believe the agent's own report once one arrives.
func TestTickFillsANodeToItsCapacityAndNoFurther(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_1", Name: "act_1", BalanceMicros: 1_000_000_000}))
	require.NoError(t, st.UpsertNode(ctx, store.Node{ID: "nod_1", AccountID: "act_1", Hostname: "h", Arch: "amd64",
		OfferCores: 4, OfferMemoryMB: 8192, OfferDiskMB: 100_000}))
	for i := 0; i < 6; i++ {
		queuedTask(t, st, "tsk_"+string(rune('a'+i)), 1, 256)
	}
	disp := &dispatchRecorder{}
	s := New(st, disp, clock.Real{}, discardLog(), 60)

	s.tick(ctx)
	require.Equal(t, 4, disp.count(), "one tick fills the node's 4 cores with four 1-core tasks, not just one")

	s.tick(ctx)
	require.Equal(t, 4, disp.count(), "a full node is offered nothing more, even before the agent reports (this was the over-dispatch)")

	// The agent reports it has 2 cores free (two of the four finished): the next tick uses exactly that.
	require.NoError(t, s.OnCapacityReport(ctx, "nod_1", api.CapacityReportEvent{FreeCores: 2, FreeMemoryMB: 4096, FreeDiskMB: 90_000}))
	s.tick(ctx)
	require.Equal(t, 6, disp.count(), "the agent's report is the truth: two more cores, two more tasks")

	queued := 0
	tasks, err := st.ListTasksByNode(ctx, "nod_1", []store.TaskState{store.TaskDispatched})
	require.NoError(t, err)
	queued = len(tasks)
	require.Equal(t, 6, queued)
}

// Different sizes: the node's memory is a limit too, and a task that does not fit is skipped rather than blocking the ones behind it.
func TestTickRespectsMemoryAndSkipsWhatDoesNotFit(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_1", Name: "act_1", BalanceMicros: 1_000_000_000}))
	require.NoError(t, st.UpsertNode(ctx, store.Node{ID: "nod_1", AccountID: "act_1", Hostname: "h", Arch: "amd64",
		OfferCores: 8, OfferMemoryMB: 1024, OfferDiskMB: 100_000}))
	queuedTask(t, st, "tsk_big", 1, 4096)  // never fits this node's memory
	queuedTask(t, st, "tsk_s1", 0.25, 512) // the two small ones together use all 1024 MB
	queuedTask(t, st, "tsk_s2", 0.25, 512)
	queuedTask(t, st, "tsk_s3", 0.25, 512) // no memory left for a third
	disp := &dispatchRecorder{}
	s := New(st, disp, clock.Real{}, discardLog(), 60)

	s.tick(ctx)
	require.Equal(t, 2, disp.count(), "two 512 MB tasks fit in 1024 MB; the 4 GB task is skipped, the third small one has no room")
}

func (d *dispatchRecorder) ids() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.tasks...)
}

// age makes a task look as if it was submitted `ago` ago.
func age(t *testing.T, st *store.PostgresStore, id string, ago time.Duration) {
	t.Helper()
	_, err := st.Pool().Exec(context.Background(), `UPDATE tasks SET created_at = now() - $2::interval WHERE id = $1`, id, fmt.Sprintf("%d seconds", int(ago.Seconds())))
	require.NoError(t, err)
}

func starvationEnv(t *testing.T, offerCores float64) (*store.PostgresStore, *dispatchRecorder, *Scheduler) {
	t.Helper()
	st := testStore(t)
	ctx := context.Background()
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_1", Name: "act_1", BalanceMicros: 1_000_000_000}))
	require.NoError(t, st.UpsertNode(ctx, store.Node{ID: "nod_1", AccountID: "act_1", Hostname: "h", Arch: "amd64",
		OfferCores: offerCores, OfferMemoryMB: 8192, OfferDiskMB: 100_000}))
	disp := &dispatchRecorder{}
	return st, disp, New(st, disp, clock.Real{}, discardLog(), 60)
}

func report(t *testing.T, s *Scheduler, freeCores float64) {
	t.Helper()
	require.NoError(t, s.OnCapacityReport(context.Background(), "nod_1", api.CapacityReportEvent{FreeCores: freeCores, FreeMemoryMB: 8192, FreeDiskMB: 100_000}))
}

// A task that needs more cores than the node has free was overtaken forever by a stream of small ones: a big task (4 cores) waited 30+ minutes while small
// ones kept the node's free cores below 4. Once it has waited long enough the node stops giving its cores to newer tasks, and the big one goes first the
// moment it fits.
func TestAWaitingBigTaskHoldsTheNodeBackAndGoesFirstWhenItFits(t *testing.T) {
	st, disp, s := starvationEnv(t, 4)
	ctx := context.Background()
	queuedTask(t, st, "tsk_big", 4, 1024)
	age(t, st, "tsk_big", 5*time.Minute)
	for i := 0; i < 6; i++ {
		queuedTask(t, st, fmt.Sprintf("tsk_small%d", i), 0.25, 64)
	}

	report(t, s, 1) // three cores are busy: the big task does not fit, but the small ones would
	s.tick(ctx)
	require.Empty(t, disp.ids(), "the node must not be filled with newer small tasks while the big one waits")

	report(t, s, 4) // the running work finished
	s.tick(ctx)
	require.Equal(t, []string{"tsk_big"}, disp.ids(), "the big task is placed first, and fills the node")
}

func TestATaskThatHasNotWaitedLongDoesNotHoldAnything(t *testing.T) {
	st, disp, s := starvationEnv(t, 4)
	queuedTask(t, st, "tsk_big", 4, 1024)
	age(t, st, "tsk_big", 10*time.Second) // well under the 90 s
	for i := 0; i < 6; i++ {
		queuedTask(t, st, fmt.Sprintf("tsk_small%d", i), 0.25, 64)
	}
	report(t, s, 1)
	s.tick(context.Background())
	require.Len(t, disp.ids(), 4, "a young big task is no reason to leave cores idle: the four 0.25-core tasks fit in one free core")
}

func TestANodeThatCouldNeverRunTheBigTaskIsNotHeldBack(t *testing.T) {
	st, disp, s := starvationEnv(t, 2) // offers 2 cores: it can never run a 4-core task
	queuedTask(t, st, "tsk_big", 4, 1024)
	age(t, st, "tsk_big", 10*time.Minute)
	for i := 0; i < 3; i++ {
		queuedTask(t, st, fmt.Sprintf("tsk_small%d", i), 0.5, 64)
	}
	s.tick(context.Background())
	require.Len(t, disp.ids(), 3, "holding this node would help no one")
}

func TestTasksOlderThanTheWaitingBigOneAreStillPlacedDuringTheHold(t *testing.T) {
	st, disp, s := starvationEnv(t, 4)
	queuedTask(t, st, "tsk_older_small", 0.5, 64)
	age(t, st, "tsk_older_small", 20*time.Minute)
	queuedTask(t, st, "tsk_big", 4, 1024)
	age(t, st, "tsk_big", 5*time.Minute)
	queuedTask(t, st, "tsk_newer_small", 0.5, 64)

	report(t, s, 1)
	s.tick(context.Background())
	require.Equal(t, []string{"tsk_older_small"}, disp.ids(), "a task that has waited even longer than the big one is not held back")
}

// A task the node just refused sat at the front of the queue: it was claimed, found to be cooling down, put back, and the node placed nothing else that tick.
func TestATaskCoolingDownForThisNodeDoesNotStallIt(t *testing.T) {
	st, disp, s := starvationEnv(t, 4)
	queuedTask(t, st, "tsk_refused", 1, 128)
	age(t, st, "tsk_refused", 10*time.Second)
	queuedTask(t, st, "tsk_next", 1, 128)
	s.mu.Lock()
	s.rejected[rejectKey{"nod_1", "tsk_refused"}] = time.Now().Add(time.Minute)
	s.mu.Unlock()

	s.tick(context.Background())
	require.Equal(t, []string{"tsk_next"}, disp.ids(), "the refused task is skipped, the next one is placed")
}

// Every node that could run a waiting big task used to empty itself for it, so two machines sat idle for a task only one of them would run
// (the load test ran at under half its cores while 270 tasks queued). One drains; the other keeps working.
func TestOnlyOneNodeDrainsForAWaitingBigTask(t *testing.T) {
	st, disp, s := starvationEnv(t, 4)
	ctx := context.Background()
	require.NoError(t, st.UpsertNode(ctx, store.Node{ID: "nod_2", AccountID: "act_1", Hostname: "h2", Arch: "amd64",
		OfferCores: 4, OfferMemoryMB: 8192, OfferDiskMB: 100_000}))
	queuedTask(t, st, "tsk_big", 4, 1024)
	age(t, st, "tsk_big", 5*time.Minute)
	for i := 0; i < 4; i++ {
		queuedTask(t, st, fmt.Sprintf("tsk_small%d", i), 0.25, 64)
	}
	report(t, s, 1)
	require.NoError(t, s.OnCapacityReport(ctx, "nod_2", api.CapacityReportEvent{FreeCores: 1, FreeMemoryMB: 8192, FreeDiskMB: 100_000}))

	s.tick(ctx)
	require.Len(t, disp.ids(), 4, "one machine drains for the big task, the other takes the small ones")
	for _, id := range []string{"nod_1", "nod_2"} {
		on, err := st.ListTasksByNode(ctx, id, []store.TaskState{store.TaskDispatched})
		require.NoError(t, err)
		require.True(t, len(on) == 0 || len(on) == 4, "all four small tasks went to a single machine, the draining one got none: %s has %d", id, len(on))
	}
}

// A node draining for a waiting big task used to sit with free cores until it was empty, however short the other work was. A newer task whose
// wall-clock limit ends before the big one could start anyway (the running task's own limit) costs it nothing, so it runs now; a long one waits.
func TestADrainingNodeStillRunsShortTasksThatEndBeforeTheBigOneCouldStart(t *testing.T) {
	st, disp, s := starvationEnv(t, 4)
	ctx := context.Background()
	queuedTask(t, st, "tsk_running", 3, 1024)
	_, err := st.Pool().Exec(ctx, `UPDATE tasks SET state = 'running', node_id = 'nod_1', started_at = now(), wall_timeout_s = 120 WHERE id = 'tsk_running'`)
	require.NoError(t, err)
	queuedTask(t, st, "tsk_big", 4, 1024)
	age(t, st, "tsk_big", 5*time.Minute)
	queuedTask(t, st, "tsk_short", 0.25, 64) // wall limit 60 s: over before the running task's 120 s
	queuedTask(t, st, "tsk_long", 0.25, 64)
	_, err = st.Pool().Exec(ctx, `UPDATE tasks SET wall_timeout_s = 600 WHERE id = 'tsk_long'`)
	require.NoError(t, err)

	report(t, s, 1)
	s.tick(ctx)
	require.Equal(t, []string{"tsk_short"}, disp.ids(), "the short task fits in the wait; the 10-minute one could delay the big task, so it waits")
}
