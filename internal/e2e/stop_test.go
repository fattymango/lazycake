//go:build integration

package e2e

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/id"
)

// containerIDs lists the engine's containers (running or not) for a task.
func containerIDs(t *testing.T, taskID string) []string {
	t.Helper()
	out, err := exec.Command("podman", "ps", "-a", "-q", "--filter", "label=lazycake.task_id="+taskID).Output()
	require.NoError(t, err)
	return strings.Fields(string(out))
}

// TestStopRunningTaskEndToEnd is task 8.12's real verification: a customer
// stops a long-running task and, with a real agent and a real container
// engine, the container actually disappears promptly, the task ends as a
// customer stop, and the money is right (customer charged for the time it
// ran, host credited, the dispatch-time hold released).
func TestStopRunningTaskEndToEnd(t *testing.T) {
	h, ctx := startHarnessWithBilling(t)

	taskID := id.New(id.Task)
	require.NoError(t, h.st.CreateTask(ctx, store.Task{
		ID: taskID, AccountID: "act_cust", State: store.TaskQueued,
		Image:        probeImage,
		Entrypoint:   []string{"sleep", "300"}, // would run for five minutes if not stopped
		Limits:       store.Limits{CPUCores: 1, MemoryMB: 128, DiskMB: 500, WallTimeoutS: 600},
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
	}))

	require.Eventually(t, func() bool {
		task, err := h.st.GetTask(ctx, taskID)
		require.NoError(t, err)
		return task.State == store.TaskRunning
	}, 30*time.Second, 200*time.Millisecond, "task never started running")
	require.NotEmpty(t, containerIDs(t, taskID), "the task's container should be running")
	time.Sleep(2 * time.Second) // let it run long enough to be worth billing

	stoppedAt := time.Now()
	got, err := h.customer.CancelTaskForAccount(ctx, "act_cust", taskID)
	require.NoError(t, err)
	require.NotNil(t, got.CancelRequestedAt, "the request is recorded straight away")

	var final store.Task
	require.Eventually(t, func() bool {
		task, err := h.st.GetTask(ctx, taskID)
		require.NoError(t, err)
		final = task
		return task.State == store.TaskCancelled
	}, 45*time.Second, 250*time.Millisecond, "the task never stopped")
	require.Less(t, time.Since(stoppedAt), 40*time.Second, "it must stop long before its 5 minutes are up")

	require.NotNil(t, final.ExitReason)
	require.Equal(t, "stopped", *final.ExitReason, "recorded as the customer's stop, not a coordinator cancel")
	require.NotNil(t, final.FinishedAt)
	require.Eventually(t, func() bool { return len(containerIDs(t, taskID)) == 0 }, 15*time.Second, 250*time.Millisecond,
		"the container must really be gone from the engine")

	// Money: charged for the time it ran, the host paid, nothing left held.
	var charged int64
	require.Eventually(t, func() bool {
		entries, err := h.st.LedgerEntriesForAccount(ctx, "act_cust")
		require.NoError(t, err)
		if len(entries) != 1 {
			return false
		}
		require.Equal(t, "charge", entries[0].Kind)
		charged = -entries[0].AmountMicros
		return true
	}, 15*time.Second, 250*time.Millisecond, "the customer was never charged for the time the task ran")
	require.Greater(t, charged, int64(0))

	cust, err := h.st.GetAccount(ctx, "act_cust")
	require.NoError(t, err)
	require.Equal(t, int64(10_000_000)-charged, cust.BalanceMicros, "balance dropped by exactly the charge")
	avail, err := h.st.AvailableBalance(ctx, "act_cust")
	require.NoError(t, err)
	require.Equal(t, cust.BalanceMicros, avail, "the hold placed at dispatch was released")
	host, err := h.st.GetAccount(ctx, "act_e2e")
	require.NoError(t, err)
	require.Greater(t, host.BalanceMicros, int64(0), "the host is paid for the work it did")

	// Stopping again is harmless and doesn't bill twice.
	_, err = h.customer.CancelTaskForAccount(ctx, "act_cust", taskID)
	require.NoError(t, err)
	entries, err := h.st.LedgerEntriesForAccount(ctx, "act_cust")
	require.NoError(t, err)
	require.Len(t, entries, 1, "a repeat stop must not create a second charge")
}
