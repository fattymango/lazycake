//go:build integration

package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/clock"
	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// recBilling records what the scheduler hands billing at each finish.
type recBilling struct {
	mu       sync.Mutex
	finished []api.TaskFinishedEvent
	nodes    []string
}

func (r *recBilling) OnTaskStarted(context.Context, string, string) error { return nil }
func (r *recBilling) OnTaskFinished(_ context.Context, nodeID string, ev api.TaskFinishedEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finished = append(r.finished, ev)
	r.nodes = append(r.nodes, nodeID)
	return nil
}
func (r *recBilling) reasons() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.finished {
		out = append(out, e.ExitReason)
	}
	return out
}

// recDispatcher records the Cancel messages sent to nodes.
type recDispatcher struct {
	mu   sync.Mutex
	sent []string // "node/task"
}

func (d *recDispatcher) Send(nodeID string, msg *lazycakev1.CoordinatorMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if c := msg.GetCancel(); c != nil {
		d.sent = append(d.sent, nodeID+"/"+c.GetTaskId())
	}
	return nil
}
func (d *recDispatcher) count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.sent) }

type cancelEnv struct {
	st   *store.PostgresStore
	s    *Scheduler
	bill *recBilling
	disp *recDispatcher
	clk  *clock.Fake
}

func newCancelEnv(t *testing.T) *cancelEnv {
	t.Helper()
	st := testStore(t)
	ctx := context.Background()
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_c", Name: "c", BalanceMicros: 10_000_000}))
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_h", Name: "h"}))
	require.NoError(t, st.UpsertNode(ctx, store.Node{ID: "nod_1", AccountID: "act_h", Hostname: "h", Arch: "amd64"}))
	clk := clock.NewFake(time.Now())
	disp := &recDispatcher{}
	bill := &recBilling{}
	s := New(st, disp, clk, discardLog(), 60)
	s.Billing = bill
	return &cancelEnv{st: st, s: s, bill: bill, disp: disp, clk: clk}
}

// runningTask puts a task on nod_1 in the given state; with cancelRequested it also records the customer's stop request.
func (e *cancelEnv) task(t *testing.T, id string, state store.TaskState, cancelRequested bool, requeueAfter *time.Time, delivery store.DeliveryMode) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, e.st.CreateTask(ctx, store.Task{
		ID: id, AccountID: "act_c", State: store.TaskQueued, Image: "x",
		Limits:       store.Limits{CPUCores: 1, MemoryMB: 128, DiskMB: 500},
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     delivery, Retry: store.Retry{MaxAttempts: 3},
	}))
	node := "nod_1"
	started := time.Now().Add(-30 * time.Second)
	upd := store.TaskUpdate{NodeID: &node, RequeueAfter: requeueAfter}
	if state == store.TaskRunning {
		upd.StartedAt = &started
	}
	require.NoError(t, e.st.TransitionTask(ctx, id, []store.TaskState{store.TaskQueued}, state, upd))
	if cancelRequested {
		ok, err := e.st.RequestTaskCancel(ctx, id)
		require.NoError(t, err)
		require.True(t, ok)
	}
}

func (e *cancelEnv) get(t *testing.T, id string) store.Task {
	t.Helper()
	task, err := e.st.GetTask(context.Background(), id)
	require.NoError(t, err)
	return task
}

// The agent reports a task it was told to stop as "cancelled", the same word
// it uses when the coordinator itself reclaims a task. A customer's stop is
// billable and the coordinator's is not, so the scheduler must tell them apart.
func TestCustomerStopIsBilledButCoordinatorCancelIsNot(t *testing.T) {
	e := newCancelEnv(t)
	ctx := context.Background()
	e.task(t, "tsk_stop", store.TaskRunning, true, nil, store.AtMostOnce)
	e.task(t, "tsk_coord", store.TaskRunning, false, nil, store.AtMostOnce)

	for _, id := range []string{"tsk_stop", "tsk_coord"} {
		require.NoError(t, e.s.OnTaskFinished(ctx, "nod_1", api.TaskFinishedEvent{TaskID: id, ExitCode: -1, ExitReason: "cancelled", At: time.Now()}))
	}

	stopped := e.get(t, "tsk_stop")
	require.Equal(t, store.TaskCancelled, stopped.State)
	require.Equal(t, "stopped", *stopped.ExitReason, "a customer's stop is recorded as such")
	coord := e.get(t, "tsk_coord")
	require.Equal(t, store.TaskCancelled, coord.State)
	require.Equal(t, "cancelled", *coord.ExitReason)

	require.ElementsMatch(t, []string{"stopped", "cancelled"}, e.bill.reasons(),
		"billing sees 'stopped' (billable) for the customer's stop and 'cancelled' (not billable) for the coordinator's")
}

// If the task finishes by itself in the instant the customer presses stop,
// it must settle exactly as a normal finish: the stop request changes nothing.
func TestTaskThatFinishesDespiteAStopRequestSettlesNormally(t *testing.T) {
	e := newCancelEnv(t)
	e.task(t, "tsk_race", store.TaskRunning, true, nil, store.AtMostOnce)

	require.NoError(t, e.s.OnTaskFinished(context.Background(), "nod_1", api.TaskFinishedEvent{TaskID: "tsk_race", ExitCode: 0, ExitReason: "exited", At: time.Now()}))

	got := e.get(t, "tsk_race")
	require.Equal(t, store.TaskSucceeded, got.State)
	require.Equal(t, "exited", *got.ExitReason)
	require.Equal(t, []string{"exited"}, e.bill.reasons(), "settled once, as an ordinary completion")
}

// A node that has gone quiet must not turn a customer's stop into a retry on
// another node (at_least_once) or into an "abandoned" host failure.
func TestOverdueTaskTheCustomerStoppedEndsAsStopped(t *testing.T) {
	e := newCancelEnv(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	e.task(t, "tsk_alo", store.TaskRunning, true, &past, store.AtLeastOnce) // would normally be retried
	e.task(t, "tsk_amo", store.TaskDispatched, true, &past, store.AtMostOnce)

	e.s.reclaimOverdue(ctx)

	alo := e.get(t, "tsk_alo")
	require.Equal(t, store.TaskCancelled, alo.State, "stopped, not requeued for a retry")
	require.Equal(t, "stopped", *alo.ExitReason)
	require.NotNil(t, alo.FinishedAt)
	amo := e.get(t, "tsk_amo")
	require.Equal(t, store.TaskCancelled, amo.State, "stopped, not abandoned")

	// Only the task that had actually started ran for time worth billing.
	require.Equal(t, []string{"stopped"}, e.bill.reasons())
	require.Equal(t, []string{"nod_1"}, e.bill.nodes)

	// And it never comes back: a second pass is a no-op.
	e.s.reclaimOverdue(ctx)
	require.Equal(t, store.TaskCancelled, e.get(t, "tsk_alo").State)
	require.Len(t, e.bill.reasons(), 1, "settled exactly once")
}

// A node still pulling the image can't act on a Cancel, and one lost on a busy
// stream is never seen, so the scheduler repeats it - but not every tick.
func TestStopIsResentUntilTheTaskFinishesButThrottled(t *testing.T) {
	e := newCancelEnv(t)
	ctx := context.Background()
	e.task(t, "tsk_r", store.TaskRunning, true, nil, store.AtMostOnce)
	e.task(t, "tsk_not_asked", store.TaskRunning, false, nil, store.AtMostOnce)

	e.s.resendCancels(ctx)
	require.Equal(t, 1, e.disp.count(), "first nudge goes out at once, and only for the task that was stopped")
	require.Equal(t, []string{"nod_1/tsk_r"}, e.disp.sent)

	e.s.resendCancels(ctx)
	require.Equal(t, 1, e.disp.count(), "not again within the same few seconds")

	e.clk.Advance(cancelResendEvery + time.Second)
	e.s.resendCancels(ctx)
	require.Equal(t, 2, e.disp.count(), "repeated once the interval has passed")

	// Once the task finishes the nudging stops.
	require.NoError(t, e.s.OnTaskFinished(ctx, "nod_1", api.TaskFinishedEvent{TaskID: "tsk_r", ExitCode: -1, ExitReason: "cancelled", At: time.Now()}))
	e.clk.Advance(time.Minute)
	e.s.resendCancels(ctx)
	require.Equal(t, 2, e.disp.count(), "no more cancels for a finished task")
}
