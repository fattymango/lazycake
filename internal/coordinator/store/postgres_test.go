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

	_, err = s.pool.Exec(ctx, `TRUNCATE gateway_traffic, gateway_totals, task_gateway_totals, task_logs, node_images, sessions, portal_credentials, tasks, nodes, api_tokens, accounts CASCADE`)
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
