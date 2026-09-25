package billing

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// Rates are the coordinator's pricing knobs (IMPLEMENTATION.md task 4.4):
//
//	price_micros = base_fee
//	             + (cpu_rate * cores + ram_rate * memory_gb + disk_rate * disk_gb) * normalised_s
//	             + net_rate * bytes / 1e9
//	             + cold_start_fee_if_pulled
type Rates struct {
	BaseFeeMicros      int64
	CPURateMicros      int64 // per core-second
	RAMRateMicros      int64 // per GB-second
	DiskRateMicros     int64 // per GB-second
	NetRateMicros      int64 // per GB transferred
	ColdStartFeeMicros int64
}

// DefaultRates are placeholder numbers - real pricing is a business
// decision outside this codebase's scope, not something to invent here.
// Chosen so a typical small task (1 core, 512MB, 1GB disk, ~10s
// normalised, no network, no cold pull) costs a few thousand micros
// (fractions of a cent), which is the right order of magnitude for a
// leftover-CPU marketplace without claiming to be an actual price list.
func DefaultRates() Rates {
	return Rates{
		BaseFeeMicros:      100,   // $0.0001 per task
		CPURateMicros:      50,    // $0.00005 per core-second
		RAMRateMicros:      10,    // $0.00001 per GB-second
		DiskRateMicros:     2,     // $0.000002 per GB-second
		NetRateMicros:      1_000, // $0.001 per GB
		ColdStartFeeMicros: 5_000, // $0.005 per cold pull
	}
}

// Ledger prices and settles each billable task finish, per PLAN.md's
// "Payout rules": a task that runs and exits (any code) is paid; a fenced
// or coordinator-cancelled task is not, since the host either didn't
// finish the work or the coordinator already moved on. An abandoned task
// (the agent vanished - task 3.4) never reaches Settle at all, since it
// never produces a TaskFinished for the scheduler to call this from.
type Ledger struct {
	Store store.Store
	Rates Rates
	Log   *slog.Logger
}

// billable reports whether a task's terminal state (as
// scheduler.terminalState would derive it) represents work the host
// should be paid for. Mirrors that function's classification deliberately
// rather than importing scheduler (which already imports billing) - the
// two are kept in sync by task 4.4's own test, which drives Settle through
// every exit reason terminalState handles.
func billable(exitReason string) bool {
	switch exitReason {
	case "fenced", "cancelled":
		return false
	default: // "exited" (any code), "error", "wall_timeout", "oom", "egress_exceeded"
		return true
	}
}

// Price computes price_micros for one task run.
func (l *Ledger) Price(limits store.Limits, normalisedS float64, bytesNet int64, coldPull bool) int64 {
	ramGB := float64(limits.MemoryMB) / 1024
	diskGB := float64(limits.DiskMB) / 1024
	perSecond := float64(l.Rates.CPURateMicros)*limits.CPUCores +
		float64(l.Rates.RAMRateMicros)*ramGB +
		float64(l.Rates.DiskRateMicros)*diskGB

	price := float64(l.Rates.BaseFeeMicros) + perSecond*normalisedS + float64(l.Rates.NetRateMicros)*(float64(bytesNet)/1e9)
	if coldPull {
		price += float64(l.Rates.ColdStartFeeMicros)
	}
	if price < 0 {
		price = 0
	}
	return int64(math.Round(price))
}

// Settle prices and, if the task's exit reason is billable, writes the
// charge/credit pair and updates both balances - all via one
// Store.SettleTask transaction. A non-billable exit reason (fenced,
// cancelled) is a deliberate no-op, not an error.
func (l *Ledger) Settle(ctx context.Context, taskID, exitReason string, exitCode int32, coldPullBytes int64, bytesNet int64, normalisedS float64) error {
	if !billable(exitReason) {
		return nil
	}

	task, err := l.Store.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("looking up task %s: %w", taskID, err)
	}
	if task.NodeID == nil {
		return fmt.Errorf("task %s has no assigned node, cannot settle", taskID)
	}
	node, err := l.Store.GetNode(ctx, *task.NodeID)
	if err != nil {
		return fmt.Errorf("looking up node %s: %w", *task.NodeID, err)
	}

	price := l.Price(task.Limits, normalisedS, bytesNet, coldPullBytes > 0)
	if err := l.Store.SettleTask(ctx, taskID, task.AccountID, node.AccountID, price); err != nil {
		return fmt.Errorf("settling task %s: %w", taskID, err)
	}
	l.Log.Info("task settled", "task_id", taskID, "customer_account", task.AccountID, "host_account", node.AccountID,
		"price_micros", price, "normalised_s", normalisedS, "cold_pull", coldPullBytes > 0)
	return nil
}
