//go:build integration

package runtime

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testRuntime(t *testing.T) *PodmanRuntime {
	t.Helper()
	sock := os.Getenv("LAZYCAKE_TEST_PODMAN_SOCKET")
	if sock == "" {
		var err error
		sock, err = DefaultSocket()
		require.NoError(t, err)
	}
	rt, err := NewPodmanRuntime(sock)
	require.NoError(t, err)
	t.Cleanup(func() { rt.Close() })
	return rt
}

func TestPullCreateStartWaitLogsRemove(t *testing.T) {
	rt := testRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const image = "docker.io/library/alpine:latest"
	size, err := rt.Pull(ctx, image)
	require.NoError(t, err)
	require.Greater(t, size, int64(0))

	id, err := rt.Create(ctx, Spec{
		Name:       "lazycake-rt-test-echo",
		Image:      image,
		Entrypoint: []string{"sh", "-c"},
		Args:       []string{"echo hello"},
		CPUCores:   1,
		MemoryMB:   128,
		Labels:     map[string]string{"lazycake.test": "1"},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Remove(context.Background(), id) })

	require.NoError(t, rt.Start(ctx, id))

	result, err := rt.Wait(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode)
	require.False(t, result.OOMKilled)

	rc, err := rt.Logs(context.Background(), id)
	require.NoError(t, err)
	buf, err := io.ReadAll(io.LimitReader(rc, 4096))
	rc.Close()
	require.NoError(t, err)
	require.True(t, bytes.Contains(buf, []byte("hello")), "log output: %q", buf)

	ids, err := rt.ListLabelled(ctx, "lazycake.test", "1")
	require.NoError(t, err)
	require.Contains(t, ids, id)

	require.NoError(t, rt.Remove(ctx, id))
}

func TestOOMKill(t *testing.T) {
	rt := testRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const image = "docker.io/library/alpine:latest"
	_, err := rt.Pull(ctx, image)
	require.NoError(t, err)

	id, err := rt.Create(ctx, Spec{
		Name:       "lazycake-rt-test-oom",
		Image:      image,
		Entrypoint: []string{"sh", "-c"},
		Args:       []string{"dd if=/dev/zero of=/dev/shm/fill bs=1M count=128"},
		CPUCores:   1,
		MemoryMB:   64,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Remove(context.Background(), id) })

	require.NoError(t, rt.Start(ctx, id))
	result, err := rt.Wait(ctx, id)
	require.NoError(t, err)
	require.True(t, result.OOMKilled, "expected OOM kill under a 64MB cap")
}
