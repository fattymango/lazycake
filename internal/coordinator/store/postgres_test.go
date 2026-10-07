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

	_, err = s.pool.Exec(ctx, `TRUNCATE task_usage_samples, node_usage, node_task_usage, node_usage_latest, task_usage_totals, gateway_traffic, gateway_totals, task_gateway_totals, task_logs, node_images, sessions, portal_credentials, tasks, nodes, api_tokens, accounts CASCADE`)
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

// A coordinator restart kills every relay connection, so the connected
// flags it left in the database must all be cleared at startup.
func TestResetGatewaysConnected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	for _, id := range []string{"gw_a", "gw_b"} {
		require.NoError(t, s.CreateGateway(ctx, Gateway{ID: id, AccountID: "act_1", Label: id}))
		require.NoError(t, s.SetGatewayConnected(ctx, id, true, []byte("k")))
	}

	require.NoError(t, s.ResetGatewaysConnected(ctx))

	gws, err := s.ListGatewaysByAccount(ctx, "act_1")
	require.NoError(t, err)
	require.Len(t, gws, 2)
	for _, g := range gws {
		require.False(t, g.Connected, "%s still marked connected", g.ID)
		require.Equal(t, []byte("k"), g.NoisePubkey, "the last-known key is kept for display")
	}
}

// RequestTaskCancel marks a task only while it is still active, keeps the
// first request time on repeats, and never marks a finished task.
func TestRequestTaskCancel(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustNode(t, s, "nod_1", "act_1")
	newTask := func(id string) {
		require.NoError(t, s.CreateTask(ctx, Task{
			ID: id, AccountID: "act_1", State: TaskQueued, Image: "x",
			Limits:       Limits{CPUCores: 1, MemoryMB: 128, DiskMB: 500},
			Requirements: Requirements{Arch: "amd64", Isolation: "podman"},
			Delivery:     AtMostOnce, Retry: Retry{MaxAttempts: 1},
		}))
	}

	newTask("tsk_a")
	ok, err := s.RequestTaskCancel(ctx, "tsk_a")
	require.NoError(t, err)
	require.True(t, ok)
	first, err := s.GetTask(ctx, "tsk_a")
	require.NoError(t, err)
	require.NotNil(t, first.CancelRequestedAt)

	ok, err = s.RequestTaskCancel(ctx, "tsk_a")
	require.NoError(t, err)
	require.True(t, ok)
	again, err := s.GetTask(ctx, "tsk_a")
	require.NoError(t, err)
	require.True(t, first.CancelRequestedAt.Equal(*again.CancelRequestedAt), "the original request time is kept")

	// A finished task is never marked.
	newTask("tsk_done")
	require.NoError(t, s.TransitionTask(ctx, "tsk_done", []TaskState{TaskQueued}, TaskSucceeded, TaskUpdate{}))
	ok, err = s.RequestTaskCancel(ctx, "tsk_done")
	require.NoError(t, err)
	require.False(t, ok, "a finished task can't be stopped")
	done, err := s.GetTask(ctx, "tsk_done")
	require.NoError(t, err)
	require.Nil(t, done.CancelRequestedAt)

	ok, err = s.RequestTaskCancel(ctx, "tsk_does_not_exist")
	require.NoError(t, err)
	require.False(t, ok)

	// ListCancelRequested returns only active, dispatched/running, requested tasks.
	node := "nod_1"
	require.NoError(t, s.TransitionTask(ctx, "tsk_a", []TaskState{TaskQueued}, TaskRunning, TaskUpdate{NodeID: &node}))
	list, err := s.ListCancelRequested(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "tsk_a", list[0].ID)
	newTask("tsk_plain")
	require.NoError(t, s.TransitionTask(ctx, "tsk_plain", []TaskState{TaskQueued}, TaskRunning, TaskUpdate{NodeID: &node}))
	list, err = s.ListCancelRequested(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1, "a running task nobody asked to stop isn't listed")
}

// --- gateway traffic (task 8.14) ---

func mustGateway(t *testing.T, s *PostgresStore, id, accountID string) {
	t.Helper()
	require.NoError(t, s.CreateGateway(context.Background(), Gateway{ID: id, AccountID: accountID, Label: id}))
}

func TestGatewayTrafficTotalsSeriesAndAttribution(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	for _, g := range []string{"gw_a", "gw_b", "gw_c"} {
		mustGateway(t, s, g, "act_1")
	}
	now := time.Now().UTC().Truncate(time.Minute)
	rep := func(gw, task, svc string, toLocal, toTask int64, final bool, at time.Time) {
		require.NoError(t, s.RecordGatewayTraffic(ctx, GatewayTrafficReport{
			GatewayID: gw, TaskID: task, Service: svc, BytesToLocal: toLocal, BytesToTask: toTask,
			Final: final, At: at, RecordTask: true,
		}))
	}

	// One task spread across THREE gateways, with different amounts, must never leak between them.
	rep("gw_a", "tsk_1", "db", 100, 1000, false, now)
	rep("gw_a", "tsk_1", "db", 11, 22, true, now.Add(time.Second)) // final: one connection
	rep("gw_b", "tsk_1", "files", 7, 70, true, now)
	rep("gw_c", "tsk_1", "api", 3, 30, true, now)
	// A second task on gateway a, bigger, an hour ago.
	rep("gw_a", "tsk_2", "db", 5000, 9000, true, now.Add(-time.Hour))

	totals, err := s.GatewayTotals(ctx, []string{"gw_a", "gw_b", "gw_c", "gw_nope"})
	require.NoError(t, err)
	require.Equal(t, TrafficTotals{BytesToLocal: 5111, BytesToTask: 10022, Connections: 2}, totals["gw_a"])
	require.Equal(t, TrafficTotals{BytesToLocal: 7, BytesToTask: 70, Connections: 1}, totals["gw_b"])
	require.Equal(t, TrafficTotals{BytesToLocal: 3, BytesToTask: 30, Connections: 1}, totals["gw_c"])
	require.NotContains(t, totals, "gw_nope", "a gateway with no traffic has no entry")

	// Per task, per gateway, per service: exact, no leakage.
	usage, err := s.TaskGatewayUsage(ctx, "tsk_1")
	require.NoError(t, err)
	require.Equal(t, []TaskGatewayTraffic{
		{GatewayID: "gw_a", Service: "db", TrafficTotals: TrafficTotals{BytesToLocal: 111, BytesToTask: 1022, Connections: 1}},
		{GatewayID: "gw_b", Service: "files", TrafficTotals: TrafficTotals{BytesToLocal: 7, BytesToTask: 70, Connections: 1}},
		{GatewayID: "gw_c", Service: "api", TrafficTotals: TrafficTotals{BytesToLocal: 3, BytesToTask: 30, Connections: 1}},
	}, usage)

	// Over time: hourly buckets, oldest first, only what's inside the window.
	series, err := s.GatewayTrafficSeries(ctx, "gw_a", now.Add(-3*time.Hour), "hour")
	require.NoError(t, err)
	require.Len(t, series, 2)
	require.True(t, series[0].At.Before(series[1].At))
	require.Equal(t, int64(5000), series[0].BytesToLocal)
	require.Equal(t, int64(111), series[1].BytesToLocal)
	older, err := s.GatewayTrafficSeries(ctx, "gw_a", now.Add(-10*time.Minute), "hour")
	require.NoError(t, err)
	require.Len(t, older, 1, "the hour-old traffic is outside a 10-minute window")
	_, err = s.GatewayTrafficSeries(ctx, "gw_a", now, "century")
	require.Error(t, err)

	// Busiest tasks, biggest first.
	top, err := s.GatewayBusiestTasks(ctx, "gw_a", now.Add(-3*time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, top, 2)
	require.Equal(t, "tsk_2", top[0].TaskID)
	require.Equal(t, int64(14000), top[0].BytesToLocal+top[0].BytesToTask)
	limited, err := s.GatewayBusiestTasks(ctx, "gw_a", now.Add(-3*time.Hour), 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)
}

// A report about a task that isn't the gateway's own still counts toward the gateway's
// totals but never creates per-task rows.
func TestGatewayTrafficForAnUnknownTaskOnlyCountsTowardTheGateway(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustGateway(t, s, "gw_a", "act_1")
	require.NoError(t, s.RecordGatewayTraffic(ctx, GatewayTrafficReport{GatewayID: "gw_a", TaskID: "tsk_stranger", Service: "db", BytesToLocal: 50, BytesToTask: 60, Final: true, RecordTask: false}))

	totals, _ := s.GatewayTotals(ctx, []string{"gw_a"})
	require.Equal(t, int64(50), totals["gw_a"].BytesToLocal)
	usage, err := s.TaskGatewayUsage(ctx, "tsk_stranger")
	require.NoError(t, err)
	require.Empty(t, usage)
	top, err := s.GatewayBusiestTasks(ctx, "gw_a", time.Now().Add(-time.Hour), 10)
	require.NoError(t, err)
	require.Empty(t, top)
}

// Deltas from one long connection (progress reports) add up, land in the right 5-minute
// bucket, and count one connection only when it closes.
func TestGatewayTrafficProgressReportsAddUp(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustGateway(t, s, "gw_a", "act_1")
	at := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < 6; i++ {
		require.NoError(t, s.RecordGatewayTraffic(ctx, GatewayTrafficReport{GatewayID: "gw_a", TaskID: "tsk_1", Service: "db", BytesToLocal: 10, BytesToTask: 100, At: at, RecordTask: true}))
	}
	usage, _ := s.TaskGatewayUsage(ctx, "tsk_1")
	require.Len(t, usage, 1)
	require.Equal(t, int64(60), usage[0].BytesToLocal)
	require.Equal(t, int64(600), usage[0].BytesToTask)
	require.Zero(t, usage[0].Connections, "still open: not counted as a connection yet")

	require.NoError(t, s.RecordGatewayTraffic(ctx, GatewayTrafficReport{GatewayID: "gw_a", TaskID: "tsk_1", Service: "db", BytesToLocal: 1, BytesToTask: 1, Final: true, At: at, RecordTask: true}))
	usage, _ = s.TaskGatewayUsage(ctx, "tsk_1")
	require.Equal(t, int64(1), usage[0].Connections)
	require.Equal(t, int64(61), usage[0].BytesToLocal)
}

// Pruning removes old 5-minute buckets but never the lifetime or per-task totals.
func TestPruningGatewayTrafficKeepsTheTotals(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustGateway(t, s, "gw_a", "act_1")
	old := time.Now().Add(-100 * 24 * time.Hour)
	require.NoError(t, s.RecordGatewayTraffic(ctx, GatewayTrafficReport{GatewayID: "gw_a", TaskID: "tsk_old", Service: "db", BytesToLocal: 7, BytesToTask: 9, Final: true, At: old, RecordTask: true}))
	require.NoError(t, s.RecordGatewayTraffic(ctx, GatewayTrafficReport{GatewayID: "gw_a", TaskID: "tsk_new", Service: "db", BytesToLocal: 1, BytesToTask: 2, Final: true, At: time.Now(), RecordTask: true}))

	deleted, err := s.PruneGatewayTraffic(ctx, time.Now().Add(-90*24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	series, _ := s.GatewayTrafficSeries(ctx, "gw_a", time.Now().Add(-200*24*time.Hour), "day")
	require.Len(t, series, 1, "the old bucket is gone from the chart data")
	totals, _ := s.GatewayTotals(ctx, []string{"gw_a"})
	require.Equal(t, int64(8), totals["gw_a"].BytesToLocal, "lifetime totals survive pruning")
	usage, _ := s.TaskGatewayUsage(ctx, "tsk_old")
	require.Len(t, usage, 1, "so does the old task's own total")
	require.Equal(t, int64(7), usage[0].BytesToLocal)
}

func mustRunningTask(t *testing.T, s *PostgresStore, id, accountID, nodeID string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.CreateTask(ctx, Task{
		ID: id, AccountID: accountID, State: TaskQueued,
		Image: "alpine@sha256:a", Limits: Limits{CPUCores: 1, MemoryMB: 256, DiskMB: 512, WallTimeoutS: 60},
		Requirements: Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     AtMostOnce, Retry: Retry{MaxAttempts: 1},
	}))
	_, err := s.pool.Exec(ctx, `UPDATE tasks SET node_id = $2 WHERE id = $1`, id, nodeID)
	require.NoError(t, err)
}

// Machine usage (task 8.15): samples average within their bucket, each task's share is
// attributed to the right machine, a task is counted for the part of the period it ran,
// and an offline period has no point at all.
func TestNodeUsageAveragesAttributesAndLeavesGaps(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustNode(t, s, "nod_1", "act_1")
	mustNode(t, s, "nod_2", "act_1")
	mustRunningTask(t, s, "tsk_a", "act_1", "nod_1")
	mustRunningTask(t, s, "tsk_b", "act_1", "nod_1")
	mustRunningTask(t, s, "tsk_other", "act_1", "nod_2")

	// A bucket start well in the past, aligned to 5 minutes, so the samples below share a bucket.
	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	rec := func(node string, at time.Time, busy float64, memUsed int64, tasks ...TaskUsageSample) {
		require.NoError(t, s.RecordNodeUsage(ctx, NodeUsageSample{
			NodeID: node, At: at, IntervalMS: 15_000, HostCPUBusy: busy, HostCPUCount: 4,
			HostMemTotal: 8 << 30, HostMemUsed: memUsed, DiskTotal: 100 << 30, DiskUsed: 40 << 30, Tasks: tasks,
		}))
	}

	// Two samples in the first bucket: task a throughout, task b only in the second.
	rec("nod_1", base.Add(10*time.Second), 0.2, 2<<30, TaskUsageSample{TaskID: "tsk_a", CPUCores: 1.0, MemoryBytes: 100 << 20})
	rec("nod_1", base.Add(25*time.Second), 0.4, 3<<30,
		TaskUsageSample{TaskID: "tsk_a", CPUCores: 0.5, MemoryBytes: 120 << 20},
		TaskUsageSample{TaskID: "tsk_b", CPUCores: 2.0, MemoryBytes: 200 << 20},
		TaskUsageSample{TaskID: "tsk_other", CPUCores: 9, MemoryBytes: 1 << 40}) // not this node's task: ignored
	// The same time on another machine must not mix in.
	rec("nod_2", base.Add(10*time.Second), 0.9, 7<<30, TaskUsageSample{TaskID: "tsk_other", CPUCores: 3, MemoryBytes: 1 << 30})
	// An hour later (the machine was offline in between).
	rec("nod_1", base.Add(time.Hour+5*time.Second), 0.1, 1<<30)

	series, err := s.NodeUsageSeries(ctx, "nod_1", base.Add(-time.Minute), "5m")
	require.NoError(t, err)
	require.Len(t, series, 2, "the offline hour has no point: a gap, not zeros")
	require.Equal(t, 2, series[0].Samples)
	require.InDelta(t, 0.3, series[0].HostCPUBusy, 1e-9, "average of 0.2 and 0.4")
	require.EqualValues(t, int64(5<<30)/2, series[0].HostMemUsed)
	require.EqualValues(t, 8<<30, series[0].HostMemTotal)
	require.EqualValues(t, 4, series[0].HostCPUCount)
	// Tasks' cores: sample 1 had 1.0, sample 2 had 0.5+2.0 (the other machine's task ignored) -> mean 1.75.
	require.InDelta(t, 1.75, series[0].TasksCPU, 1e-9)

	byTask, err := s.NodeTaskSeries(ctx, "nod_1", base.Add(-time.Minute), "5m")
	require.NoError(t, err)
	share := map[string]float64{}
	for _, p := range byTask {
		share[p.TaskID] += p.CPUCores
	}
	require.InDelta(t, 0.75, share["tsk_a"], 1e-9, "(1.0 + 0.5) / 2 samples")
	require.InDelta(t, 1.0, share["tsk_b"], 1e-9, "2.0 in one of two samples counts for half the bucket")
	require.NotContains(t, share, "tsk_other", "a task the node isn't running is never recorded against it")

	// The tasks' shares add up to the machine's task total.
	require.InDelta(t, series[0].TasksCPU, share["tsk_a"]+share["tsk_b"], 1e-9)

	// Hourly aggregation gives the same overall answer for the first hour.
	hourly, err := s.NodeUsageSeries(ctx, "nod_1", base.Add(-time.Minute), "hour")
	require.NoError(t, err)
	require.Len(t, hourly, 2)
	require.InDelta(t, 0.3, hourly[0].HostCPUBusy, 1e-9)

	// Latest reading is the newest one, per machine.
	latest, ok, err := s.NodeUsageLatest(ctx, "nod_1")
	require.NoError(t, err)
	require.True(t, ok)
	require.EqualValues(t, 1<<30, latest.Sample.HostMemUsed)
	_, ok, err = s.NodeUsageLatest(ctx, "nod_never")
	require.NoError(t, err)
	require.False(t, ok)

	// Permanent per-task summary: core-seconds = cores x interval, peak memory, and it never double counts.
	sums, err := s.TaskUsageSummaries(ctx, []string{"tsk_a", "tsk_b", "tsk_other", "tsk_none"})
	require.NoError(t, err)
	require.InDelta(t, (1.0+0.5)*15, sums["tsk_a"].CoreSeconds, 1e-9)
	require.EqualValues(t, 120<<20, sums["tsk_a"].PeakMemoryBytes)
	require.InDelta(t, 2.0*15, sums["tsk_b"].CoreSeconds, 1e-9)
	require.InDelta(t, 3*15, sums["tsk_other"].CoreSeconds, 1e-9, "its own machine did record it")
	require.NotContains(t, sums, "tsk_none")
}

func TestPruningNodeUsageKeepsTheTaskSummary(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustNode(t, s, "nod_1", "act_1")
	mustRunningTask(t, s, "tsk_a", "act_1", "nod_1")
	old := time.Now().Add(-40 * 24 * time.Hour)
	require.NoError(t, s.RecordNodeUsage(ctx, NodeUsageSample{
		NodeID: "nod_1", At: old, IntervalMS: 15_000, HostCPUCount: 2,
		Tasks: []TaskUsageSample{{TaskID: "tsk_a", CPUCores: 1, MemoryBytes: 1 << 20, TunnelToGateway: 500, TunnelToTask: 900}},
	}))
	require.NoError(t, s.RecordNodeUsage(ctx, NodeUsageSample{NodeID: "nod_1", At: time.Now(), IntervalMS: 15_000, HostCPUCount: 2}))

	n, err := s.PruneNodeUsage(ctx, time.Now().Add(-30*24*time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 3, n, "the old machine bucket, the old task bucket and the task's old reading")
	series, err := s.NodeUsageSeries(ctx, "nod_1", old.Add(-time.Hour), "hour")
	require.NoError(t, err)
	require.Len(t, series, 1, "only the recent bucket is left")

	sums, err := s.TaskUsageSummaries(ctx, []string{"tsk_a"})
	require.NoError(t, err)
	require.InDelta(t, 15, sums["tsk_a"].CoreSeconds, 1e-9)
	require.EqualValues(t, 500, sums["tsk_a"].TunnelToGateway)
	require.EqualValues(t, 900, sums["tsk_a"].TunnelToTask)
}

// Network over time (follow-up to 8.15): the agent's cumulative tunnel counters become
// per-period bytes, per task and per machine bucket; a counter reset doesn't go negative;
// the task's own readings keep heartbeat resolution; and the gateway's view of the task is charted too.
func TestTunnelTrafficBecomesDeltasPerPeriod(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustNode(t, s, "nod_1", "act_1")
	mustGateway(t, s, "gw_1", "act_1")
	mustRunningTask(t, s, "tsk_a", "act_1", "nod_1")
	mustRunningTask(t, s, "tsk_b", "act_1", "nod_1")

	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
	rec := func(at time.Time, a, aIn, b int64) {
		require.NoError(t, s.RecordNodeUsage(ctx, NodeUsageSample{
			NodeID: "nod_1", At: at, IntervalMS: 15_000, HostCPUCount: 2,
			Tasks: []TaskUsageSample{
				{TaskID: "tsk_a", CPUCores: 1, MemoryBytes: 10, TunnelToGateway: a, TunnelToTask: aIn},
				{TaskID: "tsk_b", CPUCores: 1, MemoryBytes: 10, TunnelToGateway: b},
			},
		}))
	}
	rec(base.Add(10*time.Second), 0, 0, 7)
	rec(base.Add(25*time.Second), 1000, 4000, 7)
	rec(base.Add(40*time.Second), 1500, 4500, 107)
	rec(base.Add(55*time.Second), 200, 100, 107) // tsk_a's counters restarted: what it now reads is new

	pts, err := s.NodeTaskSeries(ctx, "nod_1", base.Add(-time.Minute), "5m")
	require.NoError(t, err)
	out, in := map[string]int64{}, map[string]int64{}
	for _, p := range pts {
		out[p.TaskID] += p.TunnelOut
		in[p.TaskID] += p.TunnelIn
	}
	require.EqualValues(t, 1500+200, out["tsk_a"], "1000 + 500 + a reset to 200")
	require.EqualValues(t, 4500+100, in["tsk_a"])
	require.EqualValues(t, 107, out["tsk_b"], "7 at the first reading, then 100 more")

	readings, err := s.TaskUsageSeries(ctx, "tsk_a")
	require.NoError(t, err)
	require.Len(t, readings, 4, "one per heartbeat, not folded into a bucket")
	require.EqualValues(t, 1000, readings[1].TunnelOut)
	require.EqualValues(t, 500, readings[2].TunnelOut)
	require.EqualValues(t, 200, readings[3].TunnelOut)
	var sum int64
	for _, r := range readings {
		sum += r.TunnelOut
	}
	require.EqualValues(t, out["tsk_a"], sum, "the fine readings and the buckets agree")
	other, err := s.TaskUsageSeries(ctx, "tsk_none")
	require.NoError(t, err)
	require.Empty(t, other)

	require.NoError(t, s.RecordGatewayTraffic(ctx, GatewayTrafficReport{GatewayID: "gw_1", TaskID: "tsk_a", Service: "db", BytesToLocal: 10, BytesToTask: 20, At: base.Add(time.Minute), RecordTask: true}))
	require.NoError(t, s.RecordGatewayTraffic(ctx, GatewayTrafficReport{GatewayID: "gw_1", TaskID: "tsk_b", Service: "db", BytesToLocal: 99, BytesToTask: 99, At: base.Add(time.Minute), RecordTask: true}))
	gw, err := s.TaskGatewaySeries(ctx, "tsk_a")
	require.NoError(t, err)
	require.Len(t, gw, 1)
	require.EqualValues(t, 10, gw[0].BytesToLocal)
	require.EqualValues(t, 20, gw[0].BytesToTask)

	// Pruning removes the fine readings with the buckets.
	n, err := s.PruneNodeUsage(ctx, time.Now())
	require.NoError(t, err)
	require.Positive(t, n)
	readings, err = s.TaskUsageSeries(ctx, "tsk_a")
	require.NoError(t, err)
	require.Empty(t, readings)
}
