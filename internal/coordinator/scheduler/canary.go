package scheduler

import (
	"log/slog"
	"sync"
	"time"
)

const (
	// canaryRateLow/High bound task 5.2's injection rate: checked more
	// often while a node is unproven (or already suspicious), less often
	// once it has a track record - checking has a real cost, since a
	// canary occupies a slot that could otherwise run paying work.
	canaryRateLow  = 0.05
	canaryRateHigh = 0.005
	// canaryGrace is slack after a canary's expected_runtime_s before
	// declaring it a miss, absorbing ordinary scheduling/network jitter.
	canaryGrace = 5 * time.Second
)

// canaryRate interpolates task 5.2's injection rate linearly between
// canaryRateLow (trust 0) and canaryRateHigh (trust 1).
func canaryRate(trust float64) float64 {
	if trust <= 0 {
		return canaryRateLow
	}
	if trust >= 1 {
		return canaryRateHigh
	}
	return canaryRateLow + (canaryRateHigh-canaryRateLow)*trust
}

// CanaryTracker implements task 5.2's detection half: a canary task is
// dispatched against the platform's own gateway with a known expected
// runtime, and is checked - via the gateway's own report, not the agent's
// - for whether it actually connected and ran. "A node returning early
// without running the workload is caught by the gateway seeing no
// connection" is task 5.2's own "Done when," and is exactly what this
// tracker proves: output-hash verification of *what* the workload
// produced is not implemented (see OPEN_QUESTIONS.md) - presence, not
// correctness, of the connection is what's checked here.
type CanaryTracker struct {
	Trust *TrustTracker
	Log   *slog.Logger

	mu      sync.Mutex
	pending map[string]*canaryState
}

type canaryState struct {
	nodeID string
	seen   bool
}

// NewCanaryTracker returns an empty tracker.
func NewCanaryTracker(trust *TrustTracker, log *slog.Logger) *CanaryTracker {
	return &CanaryTracker{Trust: trust, Log: log, pending: make(map[string]*canaryState)}
}

// Expect registers taskID as a canary dispatched to nodeID, to be resolved
// after expectedRuntimeS plus canaryGrace.
func (c *CanaryTracker) Expect(taskID, nodeID string, expectedRuntimeS int) {
	c.mu.Lock()
	c.pending[taskID] = &canaryState{nodeID: nodeID}
	c.mu.Unlock()

	wait := time.Duration(expectedRuntimeS)*time.Second + canaryGrace
	time.AfterFunc(wait, func() { c.resolve(taskID) })
}

// RecordGatewayActivity marks taskID as having actually reached the
// platform gateway - call this from wherever GatewayService.ReportBytes
// lands, for every task (a no-op if taskID isn't a pending canary). Any
// nonzero byte report is proof of a real connection.
func (c *CanaryTracker) RecordGatewayActivity(taskID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.pending[taskID]; ok {
		s.seen = true
	}
}

func (c *CanaryTracker) resolve(taskID string) {
	c.mu.Lock()
	s, ok := c.pending[taskID]
	delete(c.pending, taskID)
	c.mu.Unlock()
	if !ok {
		return
	}

	if s.seen {
		c.Log.Info("canary passed", "task_id", taskID, "node_id", s.nodeID)
		c.Trust.CanaryPassed(s.nodeID)
		return
	}
	c.Log.Warn("canary failed: platform gateway never saw a connection for it", "task_id", taskID, "node_id", s.nodeID)
	c.Trust.CanaryFailed(s.nodeID)
}
