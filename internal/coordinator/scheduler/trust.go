package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// Trust score constants (IMPLEMENTATION.md task 5.3). Documented in
// README.md's "Trust score" section too - that's the copy reviewers are
// expected to read; keep the two in sync if either changes.
const (
	TrustStart         = 0.5
	TrustBanThreshold  = 0.2
	TrustMax           = 1.0
	TrustMin           = 0.0
	trustCleanRaise    = 0.01
	trustCanaryPass    = 0.02
	trustCanaryFail    = 0.30
	trustByteDivergent = 0.20
	trustSpecDrift     = 0.15
	trustAbandoned     = 0.25

	// trustHealPerHour is how fast a connected node's score climbs back toward TrustStart with time alone.
	// A ban ("stop dispatching") used to be permanent: a banned node gets no tasks, so it could never earn a
	// clean completion back, and canaries are off by default. Healing is deliberately slow (a node at 0.0
	// is dispatched to again after about an hour) and only restores the starting score, never more: trust
	// above TrustStart still has to be earned by honest work.
	trustHealPerHour = 0.2
	// trustHealPersistStep is how much healing accumulates before it is written to the database.
	trustHealPersistStep = 0.01
)

// TrustTracker holds each node's trust score in [0,1], starting at
// TrustStart. It's the sink every other fraud signal in Phase 5 (and byte
// reconciliation, task 4.3) feeds into, and the source task 5.4's
// placement score and this scheduler's own dispatch loop read from.
type TrustTracker struct {
	// Store, if set, persists every adjustment via SetNodeTrustScore -
	// best effort, logged not returned, since the in-memory map here is
	// this process's own source of truth for scheduling decisions either
	// way. Nil is fine (and is what keeps this type unit-testable without
	// a database - see TestTrustConvergence).
	Store store.Store
	Log   *slog.Logger

	mu        sync.Mutex
	scores    map[string]float64
	persisted map[string]float64 // last score written by Heal, to throttle writes
}

// NewTrustTracker returns an empty tracker.
func NewTrustTracker() *TrustTracker {
	return &TrustTracker{scores: make(map[string]float64), persisted: make(map[string]float64)}
}

// Score returns nodeID's current trust score, TrustStart if never observed.
func (t *TrustTracker) Score(nodeID string) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.scoreLocked(nodeID)
}

func (t *TrustTracker) scoreLocked(nodeID string) float64 {
	s, ok := t.scores[nodeID]
	if !ok {
		return TrustStart
	}
	return s
}

// Banned reports whether nodeID's trust score is below TrustBanThreshold -
// "stop dispatching and freeze the balance" (task 5.3).
func (t *TrustTracker) Banned(nodeID string) bool {
	return t.Score(nodeID) < TrustBanThreshold
}

func (t *TrustTracker) adjust(nodeID string, delta float64) float64 {
	t.mu.Lock()
	s := t.scoreLocked(nodeID) + delta
	if s > TrustMax {
		s = TrustMax
	}
	if s < TrustMin {
		s = TrustMin
	}
	t.scores[nodeID] = s
	t.mu.Unlock()

	if t.Store != nil {
		if err := t.Store.SetNodeTrustScore(context.Background(), nodeID, s); err != nil && t.Log != nil {
			t.Log.Warn("persisting trust score", "node_id", nodeID, "error", err)
		}
	}
	return s
}

// CleanCompletion raises trust: the host ran the task and reported an
// honest result, whatever the customer's own exit code was.
func (t *TrustTracker) CleanCompletion(nodeID string) float64 {
	return t.adjust(nodeID, trustCleanRaise)
}

// CanaryPassed raises trust: a canary task (task 5.2) - verified by the
// platform's own gateway, not the agent - completed as expected.
func (t *TrustTracker) CanaryPassed(nodeID string) float64 { return t.adjust(nodeID, trustCanaryPass) }

// CanaryFailed drops trust sharply: a canary the platform's own gateway
// expected to see run either never showed up or produced the wrong result.
func (t *TrustTracker) CanaryFailed(nodeID string) float64 { return t.adjust(nodeID, -trustCanaryFail) }

// ByteDivergence drops trust: task 4.3's three-point reconciliation flagged
// this node's reported bytes as inconsistent with the relay's and
// gateway's own counts.
func (t *TrustTracker) ByteDivergence(nodeID string) float64 {
	return t.adjust(nodeID, -trustByteDivergent)
}

// SpecDrift drops trust: task 5.1 flagged this node's measured task
// durations as inconsistent with its own established baseline.
func (t *TrustTracker) SpecDrift(nodeID string) float64 { return t.adjust(nodeID, -trustSpecDrift) }

// Abandoned drops trust: the node vanished mid-task (task 3.4's reclaimer
// had to abandon a task rather than seeing it finish).
func (t *TrustTracker) Abandoned(nodeID string) float64 { return t.adjust(nodeID, -trustAbandoned) }

// Heal moves a node's score back toward TrustStart by the time that has passed, if it is below it. Call it
// once per scheduling tick for each connected node. It returns the new score.
func (t *TrustTracker) Heal(nodeID string, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return t.Score(nodeID)
	}
	t.mu.Lock()
	cur := t.scoreLocked(nodeID)
	if cur >= TrustStart {
		t.mu.Unlock()
		return cur
	}
	next := cur + trustHealPerHour*elapsed.Hours()
	if next > TrustStart {
		next = TrustStart
	}
	t.scores[nodeID] = next
	last, ok := t.persisted[nodeID]
	write := !ok || next-last >= trustHealPersistStep || next >= TrustStart
	if write {
		t.persisted[nodeID] = next
	}
	t.mu.Unlock()

	if write && t.Store != nil {
		if err := t.Store.SetNodeTrustScore(context.Background(), nodeID, next); err != nil && t.Log != nil {
			t.Log.Warn("persisting healed trust score", "node_id", nodeID, "error", err)
		}
	}
	return next
}
