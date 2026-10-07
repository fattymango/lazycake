package scheduler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

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

// specDriftMinNormalisedS is the shortest task that says anything about a machine's speed. Below it the
// timing is dominated by container start-up and scheduling noise, not by the work: a task that takes 0.03 s
// one time and 0.3 s the next has not slowed down 10x. (Found by the production load test: six sub-second
// "hello" tasks pushed an honest machine to the ban threshold.)
const specDriftMinNormalisedS = 10.0

// specDriftMaxWorkloads bounds the memory spent per node: a node's drift is tracked for up to this many
// distinct workloads, and workloads seen after that are ignored.
const specDriftMaxWorkloads = 256

// SpecDriftTracker implements task 5.1: compare a node's measured task
// durations (normalised_s, already adjusted for its own claimed
// bench_score - see task 4.2) against its own established baseline, and
// flag it once the two diverge enough to suggest sandbagging (lying about
// speed) or oversubscription (real contention the node isn't accounting
// for), not just ordinary task-to-task variance.
//
// "Its own established baseline" is per WORKLOAD: how long this node takes for tasks of the same image,
// command, environment and size. Comparing a task against a baseline built from different tasks (a 0.3 s
// "hello" against a 90 s benchmark) is meaningless, and flagged every honest machine that ran a mix of work:
// found by the production load test, which banned both test machines within three minutes.
type SpecDriftTracker struct {
	mu    sync.Mutex
	nodes map[string]map[string]*nodeSpecStats // node -> workload -> stats
}

type nodeSpecStats struct {
	calibrating []float64 // normalised_s samples collected before baseline is set
	baseline    float64   // mean of the calibration samples, 0 until established
	ratioEWMA   float64
	samples     int
}

// NewSpecDriftTracker returns an empty tracker.
func NewSpecDriftTracker() *SpecDriftTracker {
	return &SpecDriftTracker{nodes: make(map[string]map[string]*nodeSpecStats)}
}

// Observe records one task's normalised_s for nodeID running the given workload (see WorkloadKey) and
// returns the node's current rolling ratio for that workload and whether this observation flags it.
// Ratio is meaningless (returns 1, false) until the node has completed specDriftCalibrationSamples
// tasks of that same workload to establish a baseline.
func (t *SpecDriftTracker) Observe(nodeID, workload string, normalisedS float64) (ratio float64, flagged bool) {
	if normalisedS < specDriftMinNormalisedS {
		return 1, false // too short to measure: neither calibrates nor flags
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	byWorkload, ok := t.nodes[nodeID]
	if !ok {
		byWorkload = make(map[string]*nodeSpecStats)
		t.nodes[nodeID] = byWorkload
	}
	s, ok := byWorkload[workload]
	if !ok {
		if len(byWorkload) >= specDriftMaxWorkloads {
			return 1, false
		}
		s = &nodeSpecStats{}
		byWorkload[workload] = s
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

// WorkloadKey identifies "the same kind of task" for drift purposes: the same image, command, environment
// and resource limits. Two tasks with the same key should take about the same time on the same machine.
func WorkloadKey(t store.Task) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%q\x00%q\x00%g\x00%d\x00", t.Image, t.Entrypoint, t.Args, t.Limits.CPUCores, t.Limits.MemoryMB)
	keys := make([]string, 0, len(t.Env))
	for k := range t.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\x00", k, t.Env[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
