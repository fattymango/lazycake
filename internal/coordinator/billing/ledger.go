package billing

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mkassab215/lazycake/internal/coordinator/pricing"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// Rates and DefaultRates are re-exported from internal/coordinator/pricing
// so existing callers/tests in this package don't need to import it
// separately.
type Rates = pricing.Rates

var DefaultRates = pricing.DefaultRates

// Ledger prices and settles each billable task finish, per PLAN.md's
// "Payout rules": a task that runs and exits (any code) is paid; a fenced
// or coordinator-cancelled task is not, since the host either didn't
// finish the work or the coordinator already moved on. An abandoned task
// (the agent vanished - task 3.4) never reaches Settle at all, since it
// never produces a TaskFinished for the scheduler to call this from.
// Either way, whatever hold task 4.5 placed at dispatch is released here -
// a task only ever finishes once.
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
	return pricing.Price(l.Rates, limits, normalisedS, bytesNet, coldPull)
}

// Settle prices and, if the task's exit reason is billable, writes the
// charge/credit pair and updates both balances via one Store.SettleTask
// transaction; a non-billable exit reason (fenced, cancelled) is a
// deliberate no-op there. Either way, the task's dispatch-time hold (task
// 4.5) is released, since the task is now finished one way or another.
func (l *Ledger) Settle(ctx context.Context, taskID, exitReason string, exitCode int32, coldPullBytes int64, bytesNet int64, normalisedS float64) error {
	defer func() {
		if err := l.Store.ReleaseHold(ctx, taskID); err != nil {
			l.Log.Warn("releasing hold after settlement", "task_id", taskID, "error", err)
		}
	}()

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
