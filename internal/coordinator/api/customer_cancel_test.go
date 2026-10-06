package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// cancelFixture is a customer with a registry that records what each node was
// told, and a helper to put a task in any state.
type cancelFixture struct {
	t        *testing.T
	st       *fakeStore
	srv      *CustomerServer
	registry *Registry
	sent     chan *lazycakev1.CoordinatorMessage
	bus      *events.Bus
}

func newCancelFixture(t *testing.T) *cancelFixture {
	t.Helper()
	st := newFakeStore()
	bus := events.NewBus()
	reg := NewRegistry()
	return &cancelFixture{t: t, st: st, registry: reg, bus: bus,
		srv:  &CustomerServer{Store: st, Bus: bus, Registry: reg},
		sent: make(chan *lazycakev1.CoordinatorMessage, 8)}
}

func (f *cancelFixture) task(id, account string, state store.TaskState, node string) {
	t := store.Task{ID: id, AccountID: account, State: state, Image: "alpine@sha256:abc"}
	if node != "" {
		t.NodeID = &node
	}
	require := require.New(f.t)
	require.NoError(f.st.CreateTask(context.Background(), t))
	f.st.mu.Lock()
	cur := f.st.tasks[id]
	cur.State = state
	f.st.tasks[id] = cur
	f.st.mu.Unlock()
}

func (f *cancelFixture) connect(node string) {
	ch := make(chan *lazycakev1.CoordinatorMessage, 8)
	f.registry.Add(node, ch)
	f.sent = ch
}

func codeOf(err error) codes.Code { return status.Code(err) }

func TestCancelQueuedTaskIsImmediateAndFree(t *testing.T) {
	f := newCancelFixture(t)
	f.task("tsk_q", "act_1", store.TaskQueued, "")

	got, err := f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_q")
	require.NoError(t, err)
	require.Equal(t, store.TaskCancelled, got.State)
	require.NotNil(t, got.ExitReason)
	require.Equal(t, "stopped", *got.ExitReason)
	require.NotNil(t, got.FinishedAt, "a finished task has a finish time")
	require.Empty(t, f.st.ledger, "a task that never ran is never charged")
}

func TestCancelRunningTaskRecordsTheRequestAndTellsTheNode(t *testing.T) {
	f := newCancelFixture(t)
	f.task("tsk_r", "act_1", store.TaskRunning, "nod_1")
	f.connect("nod_1")

	got, err := f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_r")
	require.NoError(t, err)
	require.Equal(t, store.TaskRunning, got.State, "it isn't finished until the node reports back")
	require.NotNil(t, got.CancelRequestedAt)

	select {
	case msg := <-f.sent:
		require.Equal(t, "tsk_r", msg.GetCancel().GetTaskId(), "the node was told to stop this task")
	default:
		t.Fatal("the node was not sent a Cancel")
	}
}

func TestCancelRunningTaskWithOfflineNodeStillRecordsTheRequest(t *testing.T) {
	f := newCancelFixture(t) // node never connected
	f.task("tsk_r", "act_1", store.TaskRunning, "nod_gone")

	got, err := f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_r")
	require.NoError(t, err, "an unreachable node must not turn into an error for the customer")
	require.NotNil(t, got.CancelRequestedAt, "the scheduler will keep trying, or finish it when the node is declared gone")
}

func TestCancelIsIdempotent(t *testing.T) {
	f := newCancelFixture(t)
	f.task("tsk_r", "act_1", store.TaskRunning, "nod_1")
	f.connect("nod_1")
	first, err := f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_r")
	require.NoError(t, err)
	second, err := f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_r")
	require.NoError(t, err)
	require.Equal(t, first.CancelRequestedAt, second.CancelRequestedAt, "the original request time is kept")

	f.task("tsk_q", "act_1", store.TaskQueued, "")
	_, err = f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_q")
	require.NoError(t, err)
	_, err = f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_q")
	require.NoError(t, err, "stopping an already-stopped task is a harmless repeat")
}

func TestCancelFinishedTaskIsRefused(t *testing.T) {
	f := newCancelFixture(t)
	for _, st := range []store.TaskState{store.TaskSucceeded, store.TaskFailed, store.TaskFenced, store.TaskAbandoned} {
		id := "tsk_" + string(st)
		f.task(id, "act_1", st, "nod_1")
		_, err := f.srv.CancelTaskForAccount(context.Background(), "act_1", id)
		require.Equal(t, codes.FailedPrecondition, codeOf(err), "state %s", st)
	}
	// A coordinator-cancelled task (not a customer stop) also counts as finished.
	f.task("tsk_cc", "act_1", store.TaskCancelled, "nod_1")
	_, err := f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_cc")
	require.Equal(t, codes.FailedPrecondition, codeOf(err))
}

func TestCancelSomeoneElsesTaskLooksLikeItDoesNotExist(t *testing.T) {
	f := newCancelFixture(t)
	f.task("tsk_x", "act_other", store.TaskRunning, "nod_1")
	f.connect("nod_1")

	_, err := f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_x")
	require.Equal(t, codes.NotFound, codeOf(err))
	select {
	case <-f.sent:
		t.Fatal("a stranger's request must never reach the node")
	default:
	}
	_, err = f.srv.CancelTaskForAccount(context.Background(), "act_1", "tsk_nope")
	require.Equal(t, codes.NotFound, codeOf(err))
}
