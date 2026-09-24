//go:build integration

package exec

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/runtime"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// fakeSender collects every AgentMessage sent, safe for concurrent use
// since streamLogs and the main run() goroutine both call Send.
type fakeSender struct {
	mu    sync.Mutex
	msgs  []*lazycakev1.AgentMessage
	bytes int64
}

func (f *fakeSender) Send(m *lazycakev1.AgentMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, m)
	if logs := m.GetLogs(); logs != nil {
		for _, l := range logs.GetLines() {
			f.bytes += int64(len(l.GetLine()))
		}
	}
}

func (f *fakeSender) finished() *lazycakev1.TaskFinished {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.msgs {
		if fin := m.GetFinished(); fin != nil {
			return fin
		}
	}
	return nil
}

func (f *fakeSender) totalLogBytes() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bytes
}

// TestLogFlood proves a container that emits far more than the 50MB per-
// task log cap does not fill the agent's memory or block the task, and
// that the task still reaches a normal finish.
func TestLogFlood(t *testing.T) {
	sock := os.Getenv("LAZYCAKE_TEST_PODMAN_SOCKET")
	if sock == "" {
		var err error
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
		Runtime: rt,
		Ledger:  capacity.NewLedger(capacity.Resources{Cores: 2, MemoryMB: 2048, DiskMB: 10000}),
		Send:    sender,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	executor.HandleDispatch(ctx, &lazycakev1.Dispatch{
		TaskId:     "tsk_flood",
		Image:      image,
		Entrypoint: []string{"sh", "-c"},
		// ~500MB of 'x' lines, well past the 50MB relay cap.
		Args: []string{"yes xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx | head -c 500000000"},
		Limits: &lazycakev1.Limits{
			CpuCores: 1, MemoryMb: 256, DiskMb: 512, WallTimeoutS: 60,
		},
	})

	require.Eventually(t, func() bool {
		return sender.finished() != nil
	}, 80*time.Second, 500*time.Millisecond, "task never reported finished")

	fin := sender.finished()
	require.Equal(t, int32(0), fin.GetExitCode())
	require.Equal(t, "exited", fin.GetExitReason())

	// The relay must have stopped well short of the full 500MB.
	require.Less(t, sender.totalLogBytes(), int64(60<<20), "relayed far more than the 50MB cap")
}
