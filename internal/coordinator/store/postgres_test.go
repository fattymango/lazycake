//go:build integration

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testStore connects to LAZYCAKE_TEST_DATABASE_URL (or the same default the
// Makefile's migrate target uses) and truncates all tables so each test
// starts clean. Migrations must already be applied.
func testStore(t *testing.T) *PostgresStore {
	t.Helper()
	url := os.Getenv("LAZYCAKE_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://lazycake:lazycake@localhost:5432/lazycake?sslmode=disable"
	}
	ctx := context.Background()
	s, err := NewPostgresStore(ctx, url)
	require.NoError(t, err)
	t.Cleanup(s.Close)

	_, err = s.pool.Exec(ctx, `TRUNCATE task_logs, node_images, sessions, portal_credentials, tasks, nodes, api_tokens, accounts CASCADE`)
	require.NoError(t, err)
	return s
}

func mustAccount(t *testing.T, s *PostgresStore, id string) {
	t.Helper()
	require.NoError(t, s.CreateAccount(context.Background(), Account{ID: id, Name: id}))
}

func mustNode(t *testing.T, s *PostgresStore, id, accountID string) {
	t.Helper()
	require.NoError(t, s.UpsertNode(context.Background(), Node{
		ID: id, AccountID: accountID, Hostname: "h", Arch: "amd64",
		OfferCores: 4, OfferMemoryMB: 8192, OfferDiskMB: 20000,
	}))
}

func TestAccountLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	mustAccount(t, s, "act_1")

	got, err := s.GetAccount(ctx, "act_1")
	require.NoError(t, err)
	require.Equal(t, int64(0), got.BalanceMicros)

	bal, err := s.AdjustBalance(ctx, "act_1", 5_000_000)
	require.NoError(t, err)
	require.Equal(t, int64(5_000_000), bal)

	bal, err = s.AdjustBalance(ctx, "act_1", -1_000_000)
	require.NoError(t, err)
	require.Equal(t, int64(4_000_000), bal)

	_, err = s.GetAccount(ctx, "act_missing")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestNodeLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")

	n := Node{
		ID: "nod_1", AccountID: "act_1", Hostname: "box", Arch: "amd64",
		CPUFlags:     []string{"avx2"},
		Capabilities: Capabilities{MemoryLimit: true, Runtime: "podman"},
		OfferCores:   4, OfferMemoryMB: 8192, OfferDiskMB: 20000,
	}
	require.NoError(t, s.UpsertNode(ctx, n))

	got, err := s.GetNode(ctx, "nod_1")
	require.NoError(t, err)
	require.Equal(t, "box", got.Hostname)
	require.True(t, got.Capabilities.MemoryLimit)
	require.Equal(t, "podman", got.Capabilities.Runtime)
	require.True(t, got.Connected)

	require.NoError(t, s.SetNodeConnected(ctx, "nod_1", false))
	got, err = s.GetNode(ctx, "nod_1")
	require.NoError(t, err)
	require.False(t, got.Connected)

	now := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, s.RecordHeartbeat(ctx, "nod_1", now))
	got, err = s.GetNode(ctx, "nod_1")
	require.NoError(t, err)
	require.WithinDuration(t, now, *got.LastHeartbeatAt, time.Second)

	require.NoError(t, s.SetNodeBenchScore(ctx, "nod_1", 1.25))
	require.NoError(t, s.SetNodeTrustScore(ctx, "nod_1", 0.7))
	got, err = s.GetNode(ctx, "nod_1")
	require.NoError(t, err)
	require.InDelta(t, 1.25, *got.BenchScore, 0.001)
	require.InDelta(t, 0.7, got.TrustScore, 0.001)

	list, err := s.ListNodes(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestTaskCreateAndGet(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")

	task := Task{
		ID: "tsk_1", AccountID: "act_1", State: TaskQueued,
		Image: "alpine@sha256:deadbeef", Entrypoint: []string{"sh", "-c"}, Args: []string{"echo hi"},
		Env: map[string]string{"FOO": "bar"}, Workdir: "/work",
		Limits:       Limits{CPUCores: 1, MemoryMB: 256, DiskMB: 512, WallTimeoutS: 60},
		Requirements: Requirements{Arch: "amd64", Isolation: "podman", Confidentiality: "none"},
		Delivery:     AtMostOnce, Retry: Retry{MaxAttempts: 1},
	}
	require.NoError(t, s.CreateTask(ctx, task))

	got, err := s.GetTask(ctx, "tsk_1")
	require.NoError(t, err)
	require.Equal(t, TaskQueued, got.State)
	require.Equal(t, "bar", got.Env["FOO"])
	require.Equal(t, []string{"sh", "-c"}, got.Entrypoint)
}

func TestClaimQueuedTaskMatchesFilter(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustNode(t, s, "nod_1", "act_1")

	require.NoError(t, s.CreateTask(ctx, Task{
		ID: "tsk_1", AccountID: "act_1", State: TaskQueued,
		Image: "alpine@sha256:a", Limits: Limits{CPUCores: 1, MemoryMB: 256, DiskMB: 512, WallTimeoutS: 60},
		Requirements: Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     AtMostOnce, Retry: Retry{MaxAttempts: 1},
	}))
	// A task that requires more memory than the node offers to test filtering.
	require.NoError(t, s.CreateTask(ctx, Task{
		ID: "tsk_2", AccountID: "act_1", State: TaskQueued,
		Image: "alpine@sha256:b", Limits: Limits{CPUCores: 1, MemoryMB: 999999, DiskMB: 512, WallTimeoutS: 60},
		Requirements: Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     AtMostOnce, Retry: Retry{MaxAttempts: 1},
	}))

	filter := CapacityFilter{Arch: "amd64", Isolations: []string{"podman"}, FreeCores: 4, FreeMemoryMB: 4096, FreeDiskMB: 10000}
	claimed, err := s.ClaimQueuedTask(ctx, "nod_1", filter, time.Now().Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, "tsk_1", claimed.ID)
	require.Equal(t, TaskReserved, claimed.State)
	require.NotNil(t, claimed.NodeID)
	require.Equal(t, "nod_1", *claimed.NodeID)

	// tsk_2 still doesn't fit; nothing left to claim.
	_, err = s.ClaimQueuedTask(ctx, "nod_1", filter, time.Now().Add(time.Minute))
	require.ErrorIs(t, err, ErrNoTask)
}

func TestClaimConcurrent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	for i := 0; i < 20; i++ {
		mustNode(t, s, idn(i), "act_1")
		require.NoError(t, s.CreateTask(ctx, Task{
			ID: idt(i), AccountID: "act_1", State: TaskQueued,
			Image: "alpine@sha256:a", Limits: Limits{CPUCores: 1, MemoryMB: 256, DiskMB: 512, WallTimeoutS: 60},
			Requirements: Requirements{Arch: "amd64", Isolation: "podman"},
			Delivery:     AtMostOnce, Retry: Retry{MaxAttempts: 1},
		}))
	}

	filter := CapacityFilter{Arch: "amd64", Isolations: []string{"podman"}, FreeCores: 4, FreeMemoryMB: 4096, FreeDiskMB: 10000}

	results := make(chan string, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			task, err := s.ClaimQueuedTask(ctx, idn(i), filter, time.Now().Add(time.Minute))
			if err != nil {
				errs <- err
				results <- ""
				return
			}
			errs <- nil
			results <- task.ID
		}(i)
	}

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		require.NoError(t, <-errs)
		id := <-results
		require.False(t, seen[id], "task %s claimed twice", id)
		seen[id] = true
	}
	require.Len(t, seen, 20)
}

func idn(i int) string { return "nod_c" + string(rune('a'+i)) }
func idt(i int) string { return "tsk_c" + string(rune('a'+i)) }

func TestTransitionTask(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	require.NoError(t, s.CreateTask(ctx, Task{
		ID: "tsk_1", AccountID: "act_1", State: TaskQueued,
		Image: "alpine@sha256:a", Limits: Limits{CPUCores: 1, MemoryMB: 256, DiskMB: 512, WallTimeoutS: 60},
		Requirements: Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     AtMostOnce, Retry: Retry{MaxAttempts: 1},
	}))

	err := s.TransitionTask(ctx, "tsk_1", []TaskState{TaskQueued}, TaskReserved, TaskUpdate{})
	require.NoError(t, err)

	// Wrong fromState should conflict.
	err = s.TransitionTask(ctx, "tsk_1", []TaskState{TaskQueued}, TaskDispatched, TaskUpdate{})
	require.ErrorIs(t, err, ErrConflict)

	exitCode := 0
	reason := "exited"
	err = s.TransitionTask(ctx, "tsk_1", []TaskState{TaskReserved}, TaskSucceeded, TaskUpdate{
		ExitCode: &exitCode, ExitReason: &reason,
	})
	require.NoError(t, err)

	got, err := s.GetTask(ctx, "tsk_1")
	require.NoError(t, err)
	require.Equal(t, TaskSucceeded, got.State)
	require.Equal(t, 0, *got.ExitCode)
	require.Equal(t, "exited", *got.ExitReason)
}

func TestLogsAppendAndList(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	require.NoError(t, s.CreateTask(ctx, Task{
		ID: "tsk_1", AccountID: "act_1", State: TaskQueued,
		Image: "alpine@sha256:a", Limits: Limits{CPUCores: 1, MemoryMB: 256, DiskMB: 512, WallTimeoutS: 60},
		Requirements: Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     AtMostOnce, Retry: Retry{MaxAttempts: 1},
	}))

	now := time.Now().UTC()
	require.NoError(t, s.AppendLogs(ctx, []LogLine{
		{TaskID: "tsk_1", Seq: 1, Stream: "stdout", At: now, Line: "hi"},
		{TaskID: "tsk_1", Seq: 2, Stream: "stdout", At: now, Line: "bye"},
	}))
	// Re-sending the same batch (as a dropped-ack retry would) must not error.
	require.NoError(t, s.AppendLogs(ctx, []LogLine{
		{TaskID: "tsk_1", Seq: 1, Stream: "stdout", At: now, Line: "hi"},
	}))

	lines, err := s.ListLogs(ctx, "tsk_1", 0)
	require.NoError(t, err)
	require.Len(t, lines, 2)
	require.Equal(t, "hi", lines[0].Line)
	require.Equal(t, "bye", lines[1].Line)
}

func TestImageCache(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustNode(t, s, "nod_1", "act_1")

	require.NoError(t, s.RecordCachedImages(ctx, "nod_1", []CachedImage{
		{Digest: "sha256:a", SizeBytes: 100, LastUsed: time.Now()},
		{Digest: "sha256:b", SizeBytes: 200, LastUsed: time.Now()},
	}))

	list, err := s.ListCachedImages(ctx, "nod_1")
	require.NoError(t, err)
	require.Len(t, list, 2)

	nodes, err := s.NodesWithImage(ctx, "sha256:a")
	require.NoError(t, err)
	require.Equal(t, []string{"nod_1"}, nodes)

	require.NoError(t, s.RemoveCachedImages(ctx, "nod_1", []string{"sha256:a"}))
	list, err = s.ListCachedImages(ctx, "nod_1")
	require.NoError(t, err)
	require.Len(t, list, 1)
}
