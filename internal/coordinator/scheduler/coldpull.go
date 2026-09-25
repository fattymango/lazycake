package scheduler

import "sync"

// maxConcurrentColdPulls caps how many nodes may be cold-pulling the same
// image digest at once, fleet-wide (task 5.4): "Cap concurrent cold pulls
// of the same digest at 3 across the fleet; further tasks wait for a warm
// node or for the pulls to complete."
const maxConcurrentColdPulls = 3

// ColdPullLimiter tracks, per image digest, which nodes are currently
// cold-pulling it. It has no opinion about which nodes are already warm -
// that's ImageCache (task 1.9's NodesWithImage) - this only bounds how
// many *new* pulls of the same digest can be in flight simultaneously.
type ColdPullLimiter struct {
	mu       sync.Mutex
	inFlight map[string]map[string]struct{} // digest -> set of node_ids currently pulling it
}

// NewColdPullLimiter returns an empty limiter.
func NewColdPullLimiter() *ColdPullLimiter {
	return &ColdPullLimiter{inFlight: make(map[string]map[string]struct{})}
}

// TryStart reserves a cold-pull slot for nodeID pulling digest. Returns
// false if the fleet-wide cap is already reached by other nodes; calling
// it again for a (digest, nodeID) pair that already holds a slot is a
// harmless idempotent success.
func (l *ColdPullLimiter) TryStart(digest, nodeID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	set, ok := l.inFlight[digest]
	if !ok {
		set = make(map[string]struct{})
		l.inFlight[digest] = set
	}
	if _, already := set[nodeID]; already {
		return true
	}
	if len(set) >= maxConcurrentColdPulls {
		return false
	}
	set[nodeID] = struct{}{}
	return true
}

// Finish releases nodeID's cold-pull slot for digest - call once the pull
// has definitely finished one way or another (this scheduler releases it
// at OnTaskStarted, since the agent's own Pull->Create->Start sequence
// means a Started message can only arrive after Pull has already
// succeeded, and again at OnTaskFinished as a safety net for a task that
// never got that far). A no-op if nodeID doesn't currently hold a slot for
// digest.
func (l *ColdPullLimiter) Finish(digest, nodeID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if set, ok := l.inFlight[digest]; ok {
		delete(set, nodeID)
		if len(set) == 0 {
			delete(l.inFlight, digest)
		}
	}
}

// InFlight returns how many nodes are currently cold-pulling digest, for
// tests/observability.
func (l *ColdPullLimiter) InFlight(digest string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.inFlight[digest])
}
