//go:build integration

package scheduler

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/clock"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

func testStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	url := os.Getenv("LAZYCAKE_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://lazycake:lazycake@localhost:5432/lazycake?sslmode=disable"
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, url)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	_, err = s.Pool().Exec(ctx, `TRUNCATE task_logs, node_images, tasks, nodes, api_tokens, accounts CASCADE`)
	require.NoError(t, err)
	return s
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// overdueTask creates a task already dispatched to nodeID whose
// requeue_after is in the past, i.e. exactly what a killed-mid-task agent
// (task 3.4's "Done when") leaves behind for the reclaimer loop to find.
func overdueTask(t *testing.T, st *store.PostgresStore, id, accountID, nodeID string, delivery store.DeliveryMode, maxAttempts, attempt int) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.CreateTask(ctx, store.Task{
		ID: id, AccountID: accountID, State: store.TaskQueued,
		Image: "docker.io/library/alpine:latest", Entrypoint: []string{"sh"},
		Limits:       store.Limits{CPUCores: 1, MemoryMB: 128, DiskMB: 500},
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     delivery, Retry: store.Retry{MaxAttempts: maxAttempts}, Attempt: attempt,
	}))
	past := time.Now().Add(-1 * time.Minute)
	require.NoError(t, st.TransitionTask(ctx, id, []store.TaskState{store.TaskQueued}, store.TaskDispatched,
		store.TaskUpdate{NodeID: &nodeID, RequeueAfter: &past}))
}

// TestAtMostOnce is task 3.4's own verify: killing an agent mid-task (here,
// simulated the same way task 3.1/3.3's tests do - a task whose
// requeue_after has already passed while still dispatched, since that's
// the coordinator's only real signal the agent is gone) leaves an
// at_most_once task abandoned, and a second reclaim pass never re-dispatches
// it (there is nothing that would - abandoned is terminal, not queued).
func TestAtMostOnce(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_1", Name: "act_1"}))
	require.NoError(t, st.UpsertNode(ctx, store.Node{ID: "nod_1", AccountID: "act_1", Hostname: "h", Arch: "amd64"}))

	overdueTask(t, st, "tsk_amo", "act_1", "nod_1", store.AtMostOnce, 1, 0)

	s := New(st, nil, clock.Real{}, discardLog(), 60)
	s.reclaimOverdue(ctx)

	task, err := st.GetTask(ctx, "tsk_amo")
	require.NoError(t, err)
	require.Equal(t, store.TaskAbandoned, task.State)

	// A second pass must be a no-op: RequeueOverdue only ever returns
	// reserved/dispatched/running tasks, and this one is neither anymore.
	s.reclaimOverdue(ctx)
	task, err = st.GetTask(ctx, "tsk_amo")
	require.NoError(t, err)
	require.Equal(t, store.TaskAbandoned, task.State, "abandoned task must never be re-dispatched")
}

func TestAtLeastOnceRetriedUntilAttemptsExhausted(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_1", Name: "act_1"}))
	require.NoError(t, st.UpsertNode(ctx, store.Node{ID: "nod_1", AccountID: "act_1", Hostname: "h", Arch: "amd64"}))

	overdueTask(t, st, "tsk_alo", "act_1", "nod_1", store.AtLeastOnce, 2, 0)

	s := New(st, nil, clock.Real{}, discardLog(), 60)
	s.reclaimOverdue(ctx)

	task, err := st.GetTask(ctx, "tsk_alo")
	require.NoError(t, err)
	require.Equal(t, store.TaskQueued, task.State, "attempt 1 of 2 must go back to queued for a fresh claim")
	require.Equal(t, 1, task.Attempt)
	require.Nil(t, task.NodeID, "node assignment must be cleared so any node can claim it")

	// Simulate it getting dispatched again and missing its lease a second
	// time - now attempts are exhausted (attempt=1, max=2, this is the
	// 2nd dispatch).
	require.NoError(t, st.TransitionTask(ctx, "tsk_alo", []store.TaskState{store.TaskQueued}, store.TaskDispatched, store.TaskUpdate{}))
	past := time.Now().Add(-1 * time.Minute)
	require.NoError(t, st.TransitionTask(ctx, "tsk_alo", []store.TaskState{store.TaskDispatched}, store.TaskDispatched,
		store.TaskUpdate{NodeID: strPtr("nod_1"), RequeueAfter: &past}))

	s.reclaimOverdue(ctx)
	task, err = st.GetTask(ctx, "tsk_alo")
	require.NoError(t, err)
	require.Equal(t, store.TaskAbandoned, task.State, "no attempts left, must be abandoned rather than requeued forever")
}

func strPtr(s string) *string { return &s }
