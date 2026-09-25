package scheduler

import "testing"

// TestTrustConvergence is task 5.3's own verify: a simulated dishonest
// node reaches the ban threshold within 50 tasks while an honest node
// stays above 0.8.
func TestTrustConvergence(t *testing.T) {
	t.Run("dishonest node reaches the ban threshold within 50 tasks", func(t *testing.T) {
		trust := NewTrustTracker()
		const nodeID = "nod_dishonest"

		banned := false
		for i := 1; i <= 50; i++ {
			// Every task reports as having run (some clean-completion
			// credit), but this node gets caught by a fraud signal on a
			// fifth of its tasks - spec drift, byte divergence, and the
			// occasional failed canary, the same mix task 5.1/4.3/5.2
			// would actually produce for a node that's really cheating
			// some of the time rather than being an obvious, every-single-
			// task offender.
			trust.CleanCompletion(nodeID)
			switch {
			case i%15 == 0:
				trust.CanaryFailed(nodeID)
			case i%5 == 0:
				trust.SpecDrift(nodeID)
			case i%7 == 0:
				trust.ByteDivergence(nodeID)
			}
			if trust.Banned(nodeID) {
				banned = true
				break
			}
		}
		if !banned {
			t.Fatalf("dishonest node never reached the ban threshold within 50 tasks (final score %v)", trust.Score(nodeID))
		}
	})

	t.Run("honest node stays above 0.8 through 50 tasks", func(t *testing.T) {
		trust := NewTrustTracker()
		const nodeID = "nod_honest"

		for i := 1; i <= 50; i++ {
			trust.CleanCompletion(nodeID)
			if i%20 == 0 {
				trust.CanaryPassed(nodeID) // rare, since canaries are sampled
			}
		}
		if score := trust.Score(nodeID); score < 0.8 {
			t.Fatalf("honest node's score dropped to %v, want >= 0.8", score)
		}
	})

	t.Run("score never leaves [0,1]", func(t *testing.T) {
		trust := NewTrustTracker()
		for i := 0; i < 100; i++ {
			trust.CanaryFailed("nod_x")
		}
		if s := trust.Score("nod_x"); s < TrustMin || s > TrustMax {
			t.Fatalf("score %v out of [0,1]", s)
		}
		trust2 := NewTrustTracker()
		for i := 0; i < 1000; i++ {
			trust2.CleanCompletion("nod_y")
			trust2.CanaryPassed("nod_y")
		}
		if s := trust2.Score("nod_y"); s > TrustMax {
			t.Fatalf("score %v exceeds max %v", s, TrustMax)
		}
	})

	t.Run("a never-observed node starts at TrustStart and isn't banned", func(t *testing.T) {
		trust := NewTrustTracker()
		if s := trust.Score("nod_new"); s != TrustStart {
			t.Fatalf("new node score = %v, want %v", s, TrustStart)
		}
		if trust.Banned("nod_new") {
			t.Fatal("a never-observed node must not start banned")
		}
	})
}
