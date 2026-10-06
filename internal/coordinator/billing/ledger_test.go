//go:build integration

package billing

import (
	"context"
	"io"
	"log/slog"
	"math"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

func testLedgerStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	url := os.Getenv("LAZYCAKE_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://lazycake:lazycake@localhost:5432/lazycake?sslmode=disable"
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, url)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	_, err = s.Pool().Exec(ctx, `TRUNCATE ledger_entries, task_meters, task_logs, node_images, tasks, nodes, api_tokens, accounts CASCADE`)
	require.NoError(t, err)
	return s
}

// expectedPrice independently reimplements task 4.4's pricing formula, so
// the test actually checks Ledger.Price against the spec rather than
// against itself.
func expectedPrice(rates Rates, limits store.Limits, normalisedS float64, bytesNet int64, coldPull bool) int64 {
	ramGB := float64(limits.MemoryMB) / 1024
	diskGB := float64(limits.DiskMB) / 1024
	p := float64(rates.BaseFeeMicros) +
		(float64(rates.CPURateMicros)*limits.CPUCores+float64(rates.RAMRateMicros)*ramGB+float64(rates.DiskRateMicros)*diskGB)*normalisedS +
		float64(rates.NetRateMicros)*(float64(bytesNet)/1e9)
	if coldPull {
		p += float64(rates.ColdStartFeeMicros)
	}
	return int64(math.Round(p))
}

// TestLedgerConsistency is task 4.4's own verify: balances after a run of
// 100 mixed tasks (varied limits, varied exit reasons including
// non-billable ones, varied bytes and cold-pull) match a hand-computed
// (independently reimplemented, not just self-referential) expectation,
// and the ledger sums to the balance for every account.
func TestLedgerConsistency(t *testing.T) {
	st := testLedgerStore(t)
	ctx := context.Background()
	rates := DefaultRates()
	ledger := &Ledger{Store: st, Rates: rates, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	const customerStart = 100_000_000_000 // $100,000 in micros - plenty of headroom
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_customer", Name: "customer", BalanceMicros: customerStart}))

	hostIDs := []string{"act_host_a", "act_host_b", "act_host_c"}
	hostStart := map[string]int64{}
	nodeIDs := []string{"nod_a", "nod_b", "nod_c"}
	benchScores := []float64{1.0, 0.5, 2.0}
	for i, hostID := range hostIDs {
		require.NoError(t, st.CreateAccount(ctx, store.Account{ID: hostID, Name: hostID, BalanceMicros: 0}))
		hostStart[hostID] = 0
		require.NoError(t, st.UpsertNode(ctx, store.Node{
			ID: nodeIDs[i], AccountID: hostID, Hostname: "h", Arch: "amd64",
			OfferCores: 8, OfferMemoryMB: 16384, OfferDiskMB: 100000,
		}))
		score := benchScores[i]
		_, err := st.Pool().Exec(ctx, `UPDATE nodes SET bench_score = $2 WHERE id = $1`, nodeIDs[i], score)
		require.NoError(t, err)
	}

	exitReasons := []string{"exited", "exited", "exited", "exited", "exited", "exited", "exited", "fenced", "cancelled", "wall_timeout"}
	limitsOptions := []store.Limits{
		{CPUCores: 0.5, MemoryMB: 256, DiskMB: 500},
		{CPUCores: 1, MemoryMB: 512, DiskMB: 1000},
		{CPUCores: 2, MemoryMB: 1024, DiskMB: 2000},
	}

	var expectedCustomerCharge int64
	expectedHostCredit := map[string]int64{}

	const numTasks = 100
	for i := 0; i < numTasks; i++ {
		taskID := "tsk_" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		nodeIdx := i % len(nodeIDs)
		nodeID := nodeIDs[nodeIdx]
		hostID := hostIDs[nodeIdx]
		limits := limitsOptions[i%len(limitsOptions)]
		exitReason := exitReasons[i%len(exitReasons)]
		exitCode := int32(i % 3) // sometimes nonzero - must still be billed when reason is "exited"
		normalisedS := float64(5 + i%20)
		bytesNet := int64(i%5) * 200_000_000 // up to 800MB
		coldPull := i%10 == 0

		require.NoError(t, st.CreateTask(ctx, store.Task{
			ID: taskID, AccountID: "act_customer", State: store.TaskQueued,
			Image: "docker.io/library/alpine:latest", Entrypoint: []string{"sh"},
			Limits:       limits,
			Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
			Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
		}))
		require.NoError(t, st.TransitionTask(ctx, taskID, []store.TaskState{store.TaskQueued}, store.TaskDispatched,
			store.TaskUpdate{NodeID: &nodeID}))

		var coldPullBytes int64
		if coldPull {
			coldPullBytes = 50_000_000
		}
		require.NoError(t, ledger.Settle(ctx, taskID, exitReason, exitCode, coldPullBytes, bytesNet, normalisedS))

		if billable(exitReason) {
			price := expectedPrice(rates, limits, normalisedS, bytesNet, coldPull)
			expectedCustomerCharge += price
			expectedHostCredit[hostID] += price
		}
	}

	customer, err := st.GetAccount(ctx, "act_customer")
	require.NoError(t, err)
	require.Equal(t, customerStart-expectedCustomerCharge, customer.BalanceMicros, "customer balance must match hand-computed total charge")

	customerEntries, err := st.LedgerEntriesForAccount(ctx, "act_customer")
	require.NoError(t, err)
	var customerLedgerSum int64
	for _, e := range customerEntries {
		customerLedgerSum += e.AmountMicros
	}
	require.Equal(t, customer.BalanceMicros-customerStart, customerLedgerSum, "customer ledger entries must sum to the balance delta")

	for _, hostID := range hostIDs {
		host, err := st.GetAccount(ctx, hostID)
		require.NoError(t, err)
		require.Equal(t, hostStart[hostID]+expectedHostCredit[hostID], host.BalanceMicros, "host %s balance must match hand-computed total credit", hostID)

		entries, err := st.LedgerEntriesForAccount(ctx, hostID)
		require.NoError(t, err)
		var sum int64
		for _, e := range entries {
			sum += e.AmountMicros
		}
		require.Equal(t, host.BalanceMicros-hostStart[hostID], sum, "host %s ledger entries must sum to the balance delta", hostID)
	}
}

// TestNonBillableExitReasonsNeverSettle is a focused check that fenced and
// cancelled tasks write nothing to the ledger at all.
func TestNonBillableExitReasonsNeverSettle(t *testing.T) {
	st := testLedgerStore(t)
	ctx := context.Background()
	ledger := &Ledger{Store: st, Rates: DefaultRates(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_c", Name: "c", BalanceMicros: 1_000_000}))
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_h", Name: "h", BalanceMicros: 0}))
	require.NoError(t, st.UpsertNode(ctx, store.Node{ID: "nod_1", AccountID: "act_h", Hostname: "h", Arch: "amd64"}))

	for _, reason := range []string{"fenced", "cancelled"} {
		taskID := "tsk_" + reason
		require.NoError(t, st.CreateTask(ctx, store.Task{
			ID: taskID, AccountID: "act_c", State: store.TaskQueued,
			Image: "x", Limits: store.Limits{CPUCores: 1, MemoryMB: 512, DiskMB: 1000},
			Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
			Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
		}))
		nodeID := "nod_1"
		require.NoError(t, st.TransitionTask(ctx, taskID, []store.TaskState{store.TaskQueued}, store.TaskDispatched, store.TaskUpdate{NodeID: &nodeID}))
		require.NoError(t, ledger.Settle(ctx, taskID, reason, -1, 0, 0, 100))
	}

	customer, err := st.GetAccount(ctx, "act_c")
	require.NoError(t, err)
	require.Equal(t, int64(1_000_000), customer.BalanceMicros, "fenced/cancelled tasks must not change the customer balance")

	entries, err := st.LedgerEntriesForAccount(ctx, "act_c")
	require.NoError(t, err)
	require.Empty(t, entries, "fenced/cancelled tasks must write no ledger entries")
}

// A customer's stop is different from the coordinator cancelling a task: the
// host really did the work until then. "stopped" must settle like any other
// finish (customer charged for the time used, host credited), while
// "cancelled" above stays free.
func TestStoppedTaskIsBilledAndTheHostIsPaid(t *testing.T) {
	st := testLedgerStore(t)
	ctx := context.Background()
	ledger := &Ledger{Store: st, Rates: DefaultRates(), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	const start = 1_000_000_000
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_c", Name: "c", BalanceMicros: start}))
	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_h", Name: "h", BalanceMicros: 0}))
	require.NoError(t, st.UpsertNode(ctx, store.Node{ID: "nod_1", AccountID: "act_h", Hostname: "h", Arch: "amd64"}))
	limits := store.Limits{CPUCores: 1, MemoryMB: 512, DiskMB: 1000}
	require.NoError(t, st.CreateTask(ctx, store.Task{
		ID: "tsk_stopped", AccountID: "act_c", State: store.TaskQueued, Image: "x", Limits: limits,
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
	}))
	nodeID := "nod_1"
	require.NoError(t, st.TransitionTask(ctx, "tsk_stopped", []store.TaskState{store.TaskQueued}, store.TaskDispatched, store.TaskUpdate{NodeID: &nodeID}))
	require.NoError(t, st.PlaceHold(ctx, "tsk_stopped", "act_c", 500_000))

	const ranFor = 12.5 // seconds it actually ran before being stopped
	require.NoError(t, ledger.Settle(ctx, "tsk_stopped", "stopped", -1, 0, 0, ranFor))

	want := expectedPrice(DefaultRates(), limits, ranFor, 0, false)
	require.Greater(t, want, int64(0))
	customer, err := st.GetAccount(ctx, "act_c")
	require.NoError(t, err)
	require.Equal(t, int64(start)-want, customer.BalanceMicros, "the customer pays for the time it ran, no more")
	host, err := st.GetAccount(ctx, "act_h")
	require.NoError(t, err)
	require.Greater(t, host.BalanceMicros, int64(0), "the host is paid for the work it did")

	entries, err := st.LedgerEntriesForAccount(ctx, "act_c")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "charge", entries[0].Kind)
	require.Equal(t, -want, entries[0].AmountMicros)

	avail, err := st.AvailableBalance(ctx, "act_c")
	require.NoError(t, err)
	require.Equal(t, customer.BalanceMicros, avail, "the dispatch-time hold was released")
}
