//go:build integration

package scheduler

import (
	"context"
	"sync"
	"testing"

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
