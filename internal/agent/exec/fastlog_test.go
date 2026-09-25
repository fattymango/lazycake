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

	executor := &Executor{
		Runtime: rt,
		Ledger:  capacity.NewLedger(capacity.Resources{Cores: 2, MemoryMB: 2048, DiskMB: 10000}),
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Run this several times: the race is timing-dependent (whether the
	// streamLogs goroutine gets scheduled before Wait returns for an
	// already-exited container), so a single pass could pass by luck even
	// with the bug present.
	for i := 0; i < 10; i++ {
		sender := &fakeSender{}
		executor.Send = sender
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

		require.True(t, sender.hasLogLineContaining("hello-from-fast-task"),
			"iteration %d: fast task's output did not survive - streamLogs/Remove race", i)
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
