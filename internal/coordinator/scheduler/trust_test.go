package scheduler

import (
	"testing"
	"time"
)

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

func TestTrustHealsWithTimeBackToTheStartingScoreAndNoFurther(t *testing.T) {
	trust := NewTrustTracker()
	for i := 0; i < 5; i++ {
		trust.CanaryFailed("nod_x")
	}
	if trust.Score("nod_x") != 0 || !trust.Banned("nod_x") {
		t.Fatalf("setup: want a banned node at 0, got %v", trust.Score("nod_x"))
	}

	// Slowly: 0.2 an hour, so half an hour is 0.1 and still banned.
	if got := trust.Heal("nod_x", 30*time.Minute); got < 0.099 || got > 0.101 || !trust.Banned("nod_x") {
		t.Fatalf("after 30 min: score %v, want about 0.1 and still banned", got)
	}
	// An hour in total lifts the ban (0.2 is the threshold; "below" bans).
	trust.Heal("nod_x", 31*time.Minute)
	if trust.Banned("nod_x") {
		t.Fatalf("after about an hour the ban must lift (score %v)", trust.Score("nod_x"))
	}
	// Never past the starting score on time alone: more than that is earned by honest work.
	trust.Heal("nod_x", 100*time.Hour)
	if got := trust.Score("nod_x"); got != TrustStart {
		t.Fatalf("score after a very long time: %v, want exactly TrustStart %v", got, TrustStart)
	}
	// A node already above the start is left alone.
	trust.CleanCompletion("nod_good")
	trust.CleanCompletion("nod_good")
	before := trust.Score("nod_good")
	trust.Heal("nod_good", 10*time.Hour)
	if trust.Score("nod_good") != before {
		t.Fatalf("healing changed a score above TrustStart: %v -> %v", before, trust.Score("nod_good"))
	}
	// No time, no change.
	trust.CanaryFailed("nod_y")
	b := trust.Score("nod_y")
	trust.Heal("nod_y", 0)
	trust.Heal("nod_y", -time.Hour)
	if trust.Score("nod_y") != b {
		t.Fatal("zero or negative elapsed time must not change a score")
	}
}

func TestPenaltiesStillBanAFraudulentNode(t *testing.T) {
	// Healing must not blunt real fraud signals: repeated penalties outrun it by a wide margin.
	trust := NewTrustTracker()
	for i := 0; i < 3; i++ {
		trust.CanaryFailed("nod_cheat")
		trust.Heal("nod_cheat", time.Minute)
	}
	if !trust.Banned("nod_cheat") {
		t.Fatalf("three failed canaries must still ban a node despite healing (score %v)", trust.Score("nod_cheat"))
	}
}
