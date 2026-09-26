//go:build integration

package exec

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/runtime"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// TestFastTaskLogsSurvive is the regression test for a race between
// streamLogs (launched as a goroutine right after Start) and the deferred
// Remove firing once Wait returns: a task fast enough to finish before
// streamLogs gets scheduled - "echo hello" against an already-pulled
// image routinely does, in well under the time it takes Go to schedule a
// new goroutine under load - used to lose its entire output, since
// Remove could delete the container before streamLogs ever attached to
// its log stream. Caught live: a real customer-portal submission of
// exactly this shape showed "no output yet" for a task that had
// genuinely printed something and exited 0.
func TestFastTaskLogsSurvive(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Run this several times: the race is timing-dependent (whether the
	// streamLogs goroutine gets scheduled before Wait returns for an
	// already-exited container), so a single pass could pass by luck even
	// with the bug present. A fresh Executor per iteration, not a shared
	// one with .Send reassigned each time: HandleDispatch's own log-
	// draining goroutine can still be running after sender.finished()
	// becomes true (finished is signalled before the deferred log-drain-
	// then-Remove cleanup, by design - see run()), so mutating a shared
	// executor's .Send for the next iteration races that still-draining
	// goroutine's calls into the *previous* iteration's sender. Real
	// agents don't have this problem (one Executor's .Send is set once at
	// startup and never reassigned) - this was purely a test artifact.
	for i := 0; i < 10; i++ {
		sender := &fakeSender{}
		executor := &Executor{
			Runtime: rt,
			Ledger:  capacity.NewLedger(capacity.Resources{Cores: 2, MemoryMB: 2048, DiskMB: 10000}),
			Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
			Send:    sender,
		}
		taskID := fmt.Sprintf("tsk_fastlog_%d", i)
		executor.HandleDispatch(ctx, &lazycakev1.Dispatch{
			TaskId:     taskID,
			Image:      image,
			Entrypoint: []string{"sh", "-c"},
			Args:       []string{"echo hello-from-fast-task"},
			Isolation:  "podman",
			Limits:     &lazycakev1.Limits{CpuCores: 1, MemoryMb: 128, DiskMb: 500, WallTimeoutS: 60},
		})

		require.Eventually(t, func() bool { return sender.finished() != nil }, 60*time.Second, 100*time.Millisecond, "task never finished")
		require.Equal(t, int32(0), sender.finished().GetExitCode())

		// "Finished" is reported before the deferred log-drain/fallback
		// cleanup necessarily completes (see run(): sendFinished happens
		// first, Remove and its preceding fallback are deferred) - a real,
		// small, acceptable gap between "task done" and "its logs are all
		// persisted," not data loss. Poll briefly for the line rather than
		// asserting the instant "finished" is visible.
		require.Eventually(t, func() bool { return sender.hasLogLineContaining("hello-from-fast-task") }, 5*time.Second, 50*time.Millisecond,
			"iteration %d: fast task's output did not survive - streamLogs/Remove race (got: %q)", i, sender.allLogLines())
	}
}

func (f *fakeSender) hasLogLineContaining(substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.msgs {
		if logs := m.GetLogs(); logs != nil {
			for _, l := range logs.GetLines() {
				if strings.Contains(l.GetLine(), substr) {
					return true
				}
			}
		}
	}
	return false
}

func (f *fakeSender) allLogLines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.msgs {
		if logs := m.GetLogs(); logs != nil {
			for _, l := range logs.GetLines() {
				out = append(out, l.GetLine())
			}
		}
	}
	return out
}
