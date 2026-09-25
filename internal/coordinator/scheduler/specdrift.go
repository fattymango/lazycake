package scheduler

import "sync"

// specDriftCalibrationSamples is how many of a node's own early
// observations establish its baseline normalised_s before drift detection
// starts - comparing against the node's own history (not some absolute
// cross-node number) is what makes this meaningful without needing any
// ground truth for how much intrinsic work a given task represents.
const specDriftCalibrationSamples = 5

// specDriftRatioThreshold flags a node once its rolling ratio of
// actual-vs-baseline normalised_s crosses this. IMPLEMENTATION.md task 5.1
// illustrates the idea with "consistently taking 3x the expected time";
// this tracker is tuned lower (comfortably under a literal 2x slowdown, to
// reliably catch "throttled to half speed" - task 5.1's own "Done when" -
// well within the 20-task budget, not just a 3x-or-worse outlier).
const specDriftRatioThreshold = 1.8

// specDriftEWMAAlpha smooths the rolling ratio so one unusually fast or
// slow task doesn't flag or clear a node on its own.
const specDriftEWMAAlpha = 0.3

// SpecDriftTracker implements task 5.1: compare a node's measured task
// durations (normalised_s, already adjusted for its own claimed
// bench_score - see task 4.2) against its own established baseline, and
// flag it once the two diverge enough to suggest sandbagging (lying about
// speed) or oversubscription (real contention the node isn't accounting
// for), not just ordinary task-to-task variance.
type SpecDriftTracker struct {
	mu    sync.Mutex
	nodes map[string]*nodeSpecStats
}

type nodeSpecStats struct {
	calibrating []float64 // normalised_s samples collected before baseline is set
	baseline    float64   // mean of the calibration samples, 0 until established
	ratioEWMA   float64
	samples     int
}

// NewSpecDriftTracker returns an empty tracker.
func NewSpecDriftTracker() *SpecDriftTracker {
	return &SpecDriftTracker{nodes: make(map[string]*nodeSpecStats)}
}

// Observe records one task's normalised_s for nodeID and returns the
// node's current rolling ratio and whether this observation flags it.
// Ratio is meaningless (returns 1, false) until the node has completed
// specDriftCalibrationSamples tasks to establish a baseline.
func (t *SpecDriftTracker) Observe(nodeID string, normalisedS float64) (ratio float64, flagged bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.nodes[nodeID]
	if !ok {
		s = &nodeSpecStats{}
		t.nodes[nodeID] = s
	}
	s.samples++

	if s.baseline == 0 {
		s.calibrating = append(s.calibrating, normalisedS)
		if len(s.calibrating) >= specDriftCalibrationSamples {
			var sum float64
			for _, v := range s.calibrating {
				sum += v
			}
			s.baseline = sum / float64(len(s.calibrating))
			s.calibrating = nil
			s.ratioEWMA = 1
		}
		return 1, false
	}

	var r float64
	if s.baseline > 0 {
		r = normalisedS / s.baseline
	}
	s.ratioEWMA = specDriftEWMAAlpha*r + (1-specDriftEWMAAlpha)*s.ratioEWMA
	return s.ratioEWMA, s.ratioEWMA > specDriftRatioThreshold
}
