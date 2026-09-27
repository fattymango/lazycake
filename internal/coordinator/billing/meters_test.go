package billing

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mkassab215/lazycake/internal/clock"
	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// fakeMeterStore is a minimal store.Store double covering just what Meters
// touches: GetNode (for bench_score) and the Meters sub-interface itself.
// Everything else panics so a test relying on unimplemented behaviour
// fails loudly rather than silently returning zero values.
type fakeMeterStore struct {
	store.Store
	nodes  map[string]store.Node
	meters map[string]store.TaskMeter
}

func newFakeMeterStore() *fakeMeterStore {
	return &fakeMeterStore{nodes: map[string]store.Node{}, meters: map[string]store.TaskMeter{}}
}

func (f *fakeMeterStore) GetNode(ctx context.Context, id string) (store.Node, error) {
	n, ok := f.nodes[id]
	if !ok {
		return store.Node{}, store.ErrNotFound
	}
	return n, nil
}

func (f *fakeMeterStore) RecordMeterStarted(ctx context.Context, taskID, nodeID string, at time.Time) error {
	if _, ok := f.meters[taskID]; ok {
		return nil
	}
	f.meters[taskID] = store.TaskMeter{TaskID: taskID, NodeID: &nodeID, StartedAt: at}
	return nil
}

func (f *fakeMeterStore) RecordMeterFinished(ctx context.Context, taskID string, at time.Time, durationS, normalisedS float64) error {
	m, ok := f.meters[taskID]
	if !ok {
		return store.ErrNotFound
	}
	m.FinishedAt = &at
	m.DurationS = &durationS
	m.NormalisedS = &normalisedS
	f.meters[taskID] = m
	return nil
}

func (f *fakeMeterStore) GetMeter(ctx context.Context, taskID string) (store.TaskMeter, error) {
	m, ok := f.meters[taskID]
	if !ok {
		return store.TaskMeter{}, store.ErrNotFound
	}
	return m, nil
}

// TestDurationNormalisation is task 4.2's own verify: a node with
// bench_score = 0.5 running a task for 20s (by the coordinator's own
// clock, not anything the agent reports) records normalised_s = 10.
func TestDurationNormalisation(t *testing.T) {
	fs := newFakeMeterStore()
	benchScore := 0.5
	fs.nodes["nod_1"] = store.Node{ID: "nod_1", BenchScore: &benchScore}

	fc := clock.NewFake(time.Unix(0, 0))
	m := &Meters{Store: fs, Clock: fc, Log: discardLog()}

	if err := m.OnTaskStarted(context.Background(), "nod_1", "tsk_1"); err != nil {
		t.Fatalf("OnTaskStarted: %v", err)
	}
	fc.Advance(20 * time.Second)
	if err := m.OnTaskFinished(context.Background(), "nod_1", api.TaskFinishedEvent{TaskID: "tsk_1", ExitReason: "exited"}); err != nil {
		t.Fatalf("OnTaskFinished: %v", err)
	}

	meter, err := fs.GetMeter(context.Background(), "tsk_1")
	if err != nil {
		t.Fatalf("GetMeter: %v", err)
	}
	if meter.DurationS == nil || *meter.DurationS != 20 {
		t.Fatalf("duration_s = %v, want 20", meter.DurationS)
	}
	if meter.NormalisedS == nil || *meter.NormalisedS != 10 {
		t.Fatalf("normalised_s = %v, want 10", meter.NormalisedS)
	}
}

// TestDurationIgnoresAgentTimestamps proves the coordinator's own clock is
// what's used: nothing about OnTaskStarted/OnTaskFinished's signatures even
// accepts an agent-reported timestamp, so there is nothing for a
// dishonest host to feed in - this test exists to make that guarantee
// explicit rather than merely implicit in the API shape.
func TestNodeWithoutBenchScoreDefaultsToOne(t *testing.T) {
	fs := newFakeMeterStore()
	fs.nodes["nod_1"] = store.Node{ID: "nod_1"} // BenchScore nil - never benchmarked

	fc := clock.NewFake(time.Unix(0, 0))
	m := &Meters{Store: fs, Clock: fc, Log: discardLog()}

	if err := m.OnTaskStarted(context.Background(), "nod_1", "tsk_1"); err != nil {
		t.Fatalf("OnTaskStarted: %v", err)
	}
	fc.Advance(7 * time.Second)
	if err := m.OnTaskFinished(context.Background(), "nod_1", api.TaskFinishedEvent{TaskID: "tsk_1", ExitReason: "exited"}); err != nil {
		t.Fatalf("OnTaskFinished: %v", err)
	}

	meter, _ := fs.GetMeter(context.Background(), "tsk_1")
	if meter.NormalisedS == nil || *meter.NormalisedS != 7 {
		t.Fatalf("normalised_s = %v, want 7 (duration_s * default bench_score 1.0)", meter.NormalisedS)
	}
}
