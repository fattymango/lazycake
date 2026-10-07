package scheduler

import (
	"fmt"
	"testing"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// TestSpecDrift is task 5.1's own verify: a node artificially throttled to
// half speed is detected within 20 tasks, while an honest node (normal
// variance around its own baseline) never falsely flags.
func TestSpecDrift(t *testing.T) {
	t.Run("throttled to half speed is detected within 20 tasks", func(t *testing.T) {
		tracker := NewSpecDriftTracker()
		const baselineNormalisedS = 10.0
		const throttledNormalisedS = 20.0 // half speed = double the duration = double normalised_s

		detected := false
		for i := 0; i < 20; i++ {
			normalisedS := baselineNormalisedS
			if i >= specDriftCalibrationSamples {
				normalisedS = throttledNormalisedS // throttling starts right after calibration
			}
			_, flagged := tracker.Observe("nod_throttled", "wl", normalisedS)
			if flagged {
				detected = true
				break
			}
		}
		if !detected {
			t.Fatal("throttled node was never flagged within 20 tasks")
		}
	})

	t.Run("an honest node with normal variance is never flagged", func(t *testing.T) {
		tracker := NewSpecDriftTracker()
		// Normal task-to-task variance around a stable baseline - never a
		// sustained multiple of it.
		samples := []float64{10, 11, 9, 10, 10, 12, 8, 10, 11, 9, 10, 10, 9, 11, 10, 12, 8, 10, 9, 11}
		for i, s := range samples {
			_, flagged := tracker.Observe("nod_honest", "wl", s)
			if flagged {
				t.Fatalf("honest node flagged at task %d (normalised_s=%v)", i, s)
			}
		}
	})

	t.Run("ratio is meaningless before calibration completes", func(t *testing.T) {
		tracker := NewSpecDriftTracker()
		for i := 0; i < specDriftCalibrationSamples-1; i++ {
			ratio, flagged := tracker.Observe("nod_new", "wl", 100)
			if flagged {
				t.Fatalf("flagged during calibration at sample %d", i)
			}
			if ratio != 1 {
				t.Fatalf("expected ratio 1 during calibration, got %v", ratio)
			}
		}
	})
}

// The regression found by the production load test: a machine that runs a MIX of work is not drifting.
func TestSpecDriftComparesLikeWithLike(t *testing.T) {
	tracker := NewSpecDriftTracker()
	// Five instant tasks establish one workload's baseline; then ninety-second tasks of a different workload
	// arrive. Judged against the instant tasks they would look 300x slower; judged against their own kind
	// (no baseline yet) they say nothing.
	for i := 0; i < 5; i++ {
		tracker.Observe("nod_mixed", "short", 12)
	}
	for i := 0; i < 30; i++ {
		if _, flagged := tracker.Observe("nod_mixed", "benchmark-90s", 90); flagged {
			t.Fatalf("a different, longer workload was flagged as drift on its observation %d", i)
		}
	}
	// A real slowdown of the SAME workload is still caught.
	flagged := false
	for i := 0; i < 20 && !flagged; i++ {
		_, flagged = tracker.Observe("nod_mixed", "short", 12*3)
	}
	if !flagged {
		t.Fatal("a machine taking 3x as long for the same workload must still be flagged")
	}
}

func TestWorkloadKeySeparatesDifferentWorkAndIgnoresEverythingElse(t *testing.T) {
	base := store.Task{Image: "alpine@sha256:a", Args: []string{"echo hi"}, Env: map[string]string{"A": "1", "B": "2"}, Limits: store.Limits{CPUCores: 1, MemoryMB: 256}}
	same := base
	same.ID = "tsk_other"
	same.Env = map[string]string{"B": "2", "A": "1"} // order of keys is not part of the work
	if WorkloadKey(base) != WorkloadKey(same) {
		t.Fatal("the same work must have the same key whatever the task id or env ordering")
	}
	for name, mutate := range map[string]func(*store.Task){
		"image":  func(x *store.Task) { x.Image = "alpine@sha256:b" },
		"args":   func(x *store.Task) { x.Args = []string{"echo bye"} },
		"env":    func(x *store.Task) { x.Env = map[string]string{"A": "9"} },
		"cores":  func(x *store.Task) { x.Limits.CPUCores = 2 },
		"memory": func(x *store.Task) { x.Limits.MemoryMB = 512 },
	} {
		other := base
		mutate(&other)
		if WorkloadKey(base) == WorkloadKey(other) {
			t.Errorf("a different %s must give a different key", name)
		}
	}
}

func TestSpecDriftMemoryIsBounded(t *testing.T) {
	tracker := NewSpecDriftTracker()
	for i := 0; i < specDriftMaxWorkloads+500; i++ {
		tracker.Observe("nod_1", fmt.Sprintf("wl-%d", i), 1)
	}
	if got := len(tracker.nodes["nod_1"]); got > specDriftMaxWorkloads {
		t.Fatalf("tracked %d workloads for one node, cap is %d", got, specDriftMaxWorkloads)
	}
}

// The second production finding: tasks that last a fraction of a second are all timing noise.
func TestSpecDriftIgnoresTasksTooShortToMeasure(t *testing.T) {
	tracker := NewSpecDriftTracker()
	// Sub-second tasks whose durations vary wildly (0.02 s to 1.5 s: container start-up jitter) must never flag,
	// however many there are, and must not count toward a baseline either.
	durations := []float64{0.03, 0.3, 0.02, 1.5, 0.05, 0.9, 0.04, 0.6, 0.02, 1.2, 0.03, 0.8}
	for i := 0; i < 200; i++ {
		if ratio, flagged := tracker.Observe("nod_busy", "hello", durations[i%len(durations)]); flagged || ratio != 1 {
			t.Fatalf("a sub-second task must say nothing (ratio %v, flagged %v) at observation %d", ratio, flagged, i)
		}
	}
	if len(tracker.nodes["nod_busy"]) != 0 {
		t.Fatal("tasks too short to measure must not even create a baseline")
	}
	// A task long enough to measure still counts.
	for i := 0; i < specDriftCalibrationSamples; i++ {
		tracker.Observe("nod_busy", "bench", specDriftMinNormalisedS)
	}
	if len(tracker.nodes["nod_busy"]) != 1 {
		t.Fatal("a task at the minimum measurable length must be tracked")
	}
}
