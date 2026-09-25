package scheduler

import (
	"fmt"
	"testing"
)

// TestColdPullLimit is task 5.4's own verify: dispatching 50 tasks with
// the same 2GB image causes at most 3 concurrent pulls, and later tasks
// land on warm nodes.
//
// This drives ColdPullLimiter directly, the same way tryPlaceOne
// (placement.go) does in the real dispatch loop: try TryStart for a
// candidate node; if the cap is already held elsewhere, move on to try a
// different node this same tick instead of blocking. A separate "warm"
// set stands in for Store.NodesWithImage, which tryPlaceOne checks first
// (a warm node never even calls TryStart).
func TestColdPullLimit(t *testing.T) {
	const digest = "sha256:one-2gb-image"
	const numNodes = 10
	const numTasks = 50
	// pullTicks models how many placement ticks a cold pull of a 2GB image
	// realistically spans before OnTaskStarted releases its slot - long
	// enough that several tasks arrive while the first pulls are still in
	// flight, which is what actually exercises the cap (a pull that
	// finishes instantly, as in a trivial simulation, never lets more than
	// one node be mid-pull at once, and never really tests the limit).
	const pullTicks = 4

	limiter := NewColdPullLimiter()
	warm := make(map[string]bool)
	pulling := make(map[string]int) // node_id -> ticks remaining until its pull finishes

	var maxObservedInFlight, coldDispatches, warmDispatches, deferred int

	for i := 0; i < numTasks; i++ {
		// End of the previous tick: age every in-flight pull by one and
		// release any that just finished.
		for nodeID, remaining := range pulling {
			remaining--
			if remaining <= 0 {
				limiter.Finish(digest, nodeID)
				warm[nodeID] = true
				delete(pulling, nodeID)
			} else {
				pulling[nodeID] = remaining
			}
		}

		dispatched := false
		for n := 0; n < numNodes; n++ {
			nodeID := fmt.Sprintf("nod_%d", n)

			if warm[nodeID] {
				warmDispatches++
				dispatched = true
				break
			}
			if _, alreadyPulling := pulling[nodeID]; alreadyPulling {
				continue // this node's own earlier pull hasn't finished yet
			}
			if !limiter.TryStart(digest, nodeID) {
				deferred++
				continue // fleet-wide cap reached, try the next candidate node
			}
			coldDispatches++
			dispatched = true
			pulling[nodeID] = pullTicks
			if inFlight := limiter.InFlight(digest); inFlight > maxObservedInFlight {
				maxObservedInFlight = inFlight
			}
			break
		}
		if !dispatched {
			// Every node is either already mid-pull or blocked by the cap
			// this tick - exactly "further tasks wait ... for the pulls to
			// complete." Not a failure; the task just tries again next tick.
			deferred++
		}
	}

	if maxObservedInFlight != maxConcurrentColdPulls {
		t.Fatalf("max concurrent cold pulls observed = %d, want exactly %d (the cap should actually be reached, not just never exceeded)", maxObservedInFlight, maxConcurrentColdPulls)
	}
	if coldDispatches > numNodes {
		t.Fatalf("expected at most %d cold pulls total (one per node, ever), got %d", numNodes, coldDispatches)
	}
	if warmDispatches == 0 {
		t.Fatal("expected later tasks to land on warm nodes, but none did")
	}
	if deferred == 0 {
		t.Fatal("expected at least one task to be deferred by the cap (with only 10 nodes and a cap of 3, some contention is expected)")
	}
	t.Logf("cold dispatches: %d, warm dispatches: %d, deferred: %d, max concurrent: %d",
		coldDispatches, warmDispatches, deferred, maxObservedInFlight)
}

// TestColdPullLimitEnforcesCapWithoutImmediateRelease is a more direct
// check of the cap itself, holding pulls open (no Finish) to prove the
// limiter actually blocks a 4th concurrent pull rather than just
// happening to never be asked for one.
func TestColdPullLimitEnforcesCapWithoutImmediateRelease(t *testing.T) {
	limiter := NewColdPullLimiter()
	const digest = "sha256:held-open"

	for i := 0; i < maxConcurrentColdPulls; i++ {
		nodeID := fmt.Sprintf("nod_%d", i)
		if !limiter.TryStart(digest, nodeID) {
			t.Fatalf("expected slot %d to succeed under the cap", i)
		}
	}
	if limiter.TryStart(digest, "nod_overflow") {
		t.Fatal("expected a 4th concurrent pull to be refused")
	}
	if got := limiter.InFlight(digest); got != maxConcurrentColdPulls {
		t.Fatalf("InFlight = %d, want %d", got, maxConcurrentColdPulls)
	}

	// Freeing one slot lets exactly one more through.
	limiter.Finish(digest, "nod_0")
	if !limiter.TryStart(digest, "nod_overflow") {
		t.Fatal("expected a slot to open up after Finish")
	}
	if limiter.TryStart(digest, "nod_overflow2") {
		t.Fatal("cap should still hold after one slot was reused")
	}
}
