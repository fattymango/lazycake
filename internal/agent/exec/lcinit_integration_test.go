//go:build integration

package exec

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/runtime"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// buildLcinit compiles the real cmd/lcinit binary into a temp dir, so this
// test exercises the actual bind-mount-and-rewrite wiring end to end
// against a real container, not a stand-in.
func buildLcinit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "lcinit")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/lcinit")
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "building lcinit: %s", output)
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	// internal/agent/exec -> repo root
	return filepath.Join(wd, "..", "..", "..")
}

// TestExecutorWithLcinit is the wiring sanity check for task 3.6's
// integration into exec.Executor (beyond cmd/lcinit's own TestInitDeadline,
// which only proves lcinit itself works in isolation): a real container
// dispatched with LcinitPath set actually runs through
// /.lazycake/init and still reports its own exit code correctly, proving
// the bind mount and rewritten entrypoint round-trip for real.
func TestExecutorWithLcinit(t *testing.T) {
	lcinitPath := buildLcinit(t)

	sock := os.Getenv("LAZYCAKE_TEST_PODMAN_SOCKET")
	var err error
	if sock == "" {
		sock, err = runtime.DefaultSocket()
		require.NoError(t, err)
	}
	rt, err := runtime.NewPodmanRuntime(sock)
	require.NoError(t, err)
	defer rt.Close()

	const image = "docker.io/library/alpine:latest"
	_, err = rt.Pull(context.Background(), image)
	require.NoError(t, err)

	sender := &fakeSender{}
	executor := &Executor{
		Runtime:    rt,
		Ledger:     capacity.NewLedger(capacity.Resources{Cores: 2, MemoryMB: 2048, DiskMB: 10000}),
		Send:       sender,
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		LcinitPath: lcinitPath,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	executor.HandleDispatch(ctx, &lazycakev1.Dispatch{
		TaskId:     "tsk_lcinit",
		Image:      image,
		Entrypoint: []string{"sh", "-c"},
		Args:       []string{"exit 7"},
		Isolation:  "podman",
		Limits:     &lazycakev1.Limits{CpuCores: 1, MemoryMb: 128, DiskMb: 500, WallTimeoutS: 20},
	})

	require.Eventually(t, func() bool { return sender.finished() != nil }, 25*time.Second, 200*time.Millisecond, "task never finished")

	fin := sender.finished()
	require.Equal(t, int32(7), fin.GetExitCode(), "lcinit must pass the wrapped command's real exit code through")
	require.Equal(t, "exited", fin.GetExitReason())
}
