package scheduler

import "testing"

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
			_, flagged := tracker.Observe("nod_throttled", normalisedS)
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
			_, flagged := tracker.Observe("nod_honest", s)
			if flagged {
				t.Fatalf("honest node flagged at task %d (normalised_s=%v)", i, s)
			}
		}
	})

	t.Run("ratio is meaningless before calibration completes", func(t *testing.T) {
		tracker := NewSpecDriftTracker()
		for i := 0; i < specDriftCalibrationSamples-1; i++ {
			ratio, flagged := tracker.Observe("nod_new", 100)
			if flagged {
				t.Fatalf("flagged during calibration at sample %d", i)
			}
			if ratio != 1 {
				t.Fatalf("expected ratio 1 during calibration, got %v", ratio)
			}
		}
	})
}
