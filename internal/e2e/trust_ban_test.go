package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/id"
)

// The production load test banned both test machines for ordinary behaviour and a ban was forever: a banned
// node got no tasks, so it could never earn trust back. A ban must hold while it lasts and end with time.
func TestABannedNodeIsNotDispatchedToButHealsWithTime(t *testing.T) {
	h, ctx := startHarness(t)
	nodes, err := h.st.ListNodes(ctx)
	require.NoError(t, err)
	nodeID := nodes[0].ID

	// Drive the node's trust to zero: well below the 0.2 ban threshold.
	for i := 0; i < 4; i++ {
		h.sched.Trust.CanaryFailed(nodeID)
	}
	require.True(t, h.sched.Trust.Banned(nodeID))

	taskID := id.New(id.Task)
	require.NoError(t, h.st.CreateTask(ctx, store.Task{
		ID: taskID, AccountID: "act_e2e", State: store.TaskQueued, Image: probeImage,
		Entrypoint: []string{"sh", "-c"}, Args: []string{"echo healed"},
		Limits:       store.Limits{CPUCores: 1, MemoryMB: 128, DiskMB: 500, WallTimeoutS: 30},
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
	}))

	// While banned, nothing is dispatched.
	require.Never(t, func() bool {
		task, err := h.st.GetTask(ctx, taskID)
		return err != nil || task.State != store.TaskQueued
	}, 2*time.Second, 200*time.Millisecond, "a banned node must not receive tasks")

	// Time passes (the real healing rate is 0.2 an hour: two hours is plenty).
	h.sched.Trust.Heal(nodeID, 2*time.Hour)
	require.False(t, h.sched.Trust.Banned(nodeID), "two hours of healing must lift the ban")

	require.Eventually(t, func() bool {
		task, err := h.st.GetTask(ctx, taskID)
		return err == nil && task.State == store.TaskSucceeded
	}, 30*time.Second, 200*time.Millisecond, "once healed, the node must be dispatched to again")
}
