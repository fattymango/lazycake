// Package billing computes and persists the numbers IMPLEMENTATION.md's
// Phase 4 ("every task produces a defensible bill and a host credit") is
// built on: task duration and its bench-score-normalised equivalent (task
// 4.2), byte reconciliation across the three parties who each saw a task's
// traffic (task 4.3), and eventually pricing and the ledger itself (task
// 4.4). It depends on store.Store and clock.Clock only through their
// interfaces, and is driven by the scheduler rather than the other way
// around - scheduler.Scheduler calls into Meters from its own TaskEvents
// handlers, so billing never needs to know about gRPC, the agent stream,
// or dispatch at all.
package billing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mkassab215/lazycake/internal/clock"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// Meters records task_meters rows purely from the coordinator's own
// receive-time clock (task 4.2's explicit requirement - never from
// agent-reported timestamps, which a dishonest or clock-skewed host could
// inflate or deflate to change what it gets billed or credited).
type Meters struct {
	Store store.Store
	Clock clock.Clock
	Log   *slog.Logger
	// Reconciler, if set, accumulates the three-point byte reconciliation
	// (task 4.3) alongside duration metering. Nil disables it entirely -
	// useful for tests that only care about duration/normalisation.
	Reconciler *Reconciler
}

func (m *Meters) now() time.Time {
	if m.Clock == nil {
		return time.Now()
	}
	return m.Clock.Now()
}

// OnTaskStarted opens taskID's meter row at the coordinator's current
// time. Call this from wherever TaskStarted is handled, before the wire
// message's own (agent-reported) timestamp is used for anything billing
// related.
func (m *Meters) OnTaskStarted(ctx context.Context, nodeID, taskID string) error {
	if err := m.Store.RecordMeterStarted(ctx, taskID, nodeID, m.now()); err != nil {
		return fmt.Errorf("recording meter started: %w", err)
	}
	return nil
}

// OnTaskFinished closes taskID's meter row: duration_s is the coordinator's
// own finish time minus its own start time, and normalised_s = duration_s
// * the node's current bench_score (1.0 if the node has never
// benchmarked - see internal/agent/bench and task 4.1 - rather than
// silently zeroing out its normalised duration).
func (m *Meters) OnTaskFinished(ctx context.Context, nodeID, taskID string, bytesAgent int64) error {
	if m.Reconciler != nil {
		m.Reconciler.RecordAgentBytes(taskID, bytesAgent)
		m.Reconciler.Finalize(taskID)
	}

	meter, err := m.Store.GetMeter(ctx, taskID)
	if err != nil {
		return fmt.Errorf("looking up meter for %s: %w", taskID, err)
	}

	node, err := m.Store.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("looking up node %s for bench score: %w", nodeID, err)
	}
	benchScore := 1.0
	if node.BenchScore != nil {
		benchScore = *node.BenchScore
	}

	finishedAt := m.now()
	durationS := finishedAt.Sub(meter.StartedAt).Seconds()
	if durationS < 0 {
		durationS = 0
	}
	normalisedS := durationS * benchScore

	if err := m.Store.RecordMeterFinished(ctx, taskID, finishedAt, durationS, normalisedS); err != nil {
		return fmt.Errorf("recording meter finished: %w", err)
	}
	m.Log.Info("task metered", "task_id", taskID, "node_id", nodeID,
		"duration_s", durationS, "bench_score", benchScore, "normalised_s", normalisedS)
	return nil
}
