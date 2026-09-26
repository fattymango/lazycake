//go:build integration

package reconcile

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/agent/runtime"
)

func testRuntime(t *testing.T) *runtime.PodmanRuntime {
	t.Helper()
	sock := os.Getenv("LAZYCAKE_TEST_PODMAN_SOCKET")
	if sock == "" {
		var err error
		sock, err = runtime.DefaultSocket()
		require.NoError(t, err)
	}
	rt, err := runtime.NewPodmanRuntime(sock)
	require.NoError(t, err)
	t.Cleanup(func() { rt.Close() })
	return rt
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func spawn(t *testing.T, rt *runtime.PodmanRuntime, name, taskID, agentID, instanceID, bootID string) string {
	t.Helper()
	ctx := context.Background()
	id, err := rt.Create(ctx, runtime.Spec{
		Name:       name,
		Image:      "docker.io/library/alpine:latest",
		Entrypoint: []string{"sh", "-c"},
		Args:       []string{"sleep 300"},
		CPUCores:   1, MemoryMB: 64,
		Labels: map[string]string{
			taskIDLabel:     taskID,
			agentIDLabel:    agentID,
			instanceIDLabel: instanceID,
			bootIDLabel:     bootID,
		},
	})
	require.NoError(t, err)
	require.NoError(t, rt.Start(ctx, id))
	t.Cleanup(func() { _ = rt.Remove(context.Background(), id) })
	return id
}

// TestStartupSweep is task 3.7's own verify: containers left running from
// a previous run - whether a clean prior process (different instance_id)
// or one that survived a hard reboot (different boot_id) - are killed
// before the sweep returns, and a container that actually does belong to
// the current run is left alone.
func TestStartupSweep(t *testing.T) {
	rt := testRuntime(t)
	ctx := context.Background()

	_, err := rt.Pull(ctx, "docker.io/library/alpine:latest")
	require.NoError(t, err)

	spawn(t, rt, "lazycake-sweep-stale-instance", "tsk_stale1", "agent_a", "ins_old", "boot_new")
	spawn(t, rt, "lazycake-sweep-stale-boot", "tsk_stale2", "agent_a", "ins_new", "boot_old")
	spawn(t, rt, "lazycake-sweep-current", "tsk_current", "agent_a", "ins_new", "boot_new")
	spawn(t, rt, "lazycake-sweep-sibling", "tsk_sibling", "agent_b", "ins_old", "boot_old")

	require.NoError(t, Sweep(ctx, rt, "agent_a", "ins_new", "boot_new", discardLog()))

	require.False(t, existsByTaskID(t, rt, "tsk_stale1"), "container from a different instance_id must be killed")
	require.False(t, existsByTaskID(t, rt, "tsk_stale2"), "container from a different boot_id must be killed")
	require.True(t, existsByTaskID(t, rt, "tsk_current"), "a container matching this run's instance_id and boot_id must be left alone")
	require.True(t, existsByTaskID(t, rt, "tsk_sibling"), "a container labelled with a different agent_id must be left alone even if its instance/boot id looks stale - it belongs to a sibling agent sharing this engine, not a previous run of this one")
}

func existsByTaskID(t *testing.T, rt *runtime.PodmanRuntime, taskID string) bool {
	t.Helper()
	ids, err := rt.ListLabelled(context.Background(), taskIDLabel, taskID)
	require.NoError(t, err)
	return len(ids) > 0
}
