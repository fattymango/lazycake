package billing

import "testing"

// TestReconciliation is task 4.3's own verify: an agent reporting inflated
// byte counts is flagged while honest agents (all three sources roughly
// agree) are not.
func TestReconciliation(t *testing.T) {
	t.Run("honest agent, all three sources agree", func(t *testing.T) {
		rec := Reconcile("tsk_honest", "nod_1", 1_000_000, 1_001_000, 999_500)
		if rec.Flagged {
			t.Fatalf("honest agent flagged: %+v", rec)
		}
		if rec.DivergencePct > 0.02 {
			t.Fatalf("divergence_pct = %v, want <= 2%%", rec.DivergencePct)
		}
	})

	t.Run("dishonest agent inflating its own byte count is flagged", func(t *testing.T) {
		// The relay and gateway - neither of which the agent controls -
		// both saw ~1MB actually move; the agent claims 3x that.
		rec := Reconcile("tsk_dishonest", "nod_2", 3_000_000, 1_000_000, 1_010_000)
		if !rec.Flagged {
			t.Fatalf("inflated agent report not flagged: %+v", rec)
		}
		if rec.DivergencePct <= 0.02 {
			t.Fatalf("divergence_pct = %v, want > 2%%", rec.DivergencePct)
		}
	})

	t.Run("divergence_pct is spread over max, not some other base", func(t *testing.T) {
		// max=100, min=80: (100-80)/100 = 20%.
		rec := Reconcile("tsk_x", "nod_1", 100, 90, 80)
		if got, want := rec.DivergencePct, 0.20; got != want {
			t.Fatalf("divergence_pct = %v, want %v", got, want)
		}
	})

	t.Run("all zero never divides by zero", func(t *testing.T) {
		rec := Reconcile("tsk_notunnel", "nod_1", 0, 0, 0)
		if rec.Flagged {
			t.Fatalf("a task with no tunnel traffic at all must not be flagged: %+v", rec)
		}
	})

	t.Run("exactly at the 2%% threshold is not flagged, just over is", func(t *testing.T) {
		atThreshold := Reconcile("tsk_at", "nod_1", 100, 98, 100) // (100-98)/100 = 2.0%
		if atThreshold.Flagged {
			t.Fatalf("exactly 2%% divergence must not be flagged (threshold is exclusive): %+v", atThreshold)
		}
		overThreshold := Reconcile("tsk_over", "nod_1", 1000, 979, 1000) // (1000-979)/1000 = 2.1%
		if !overThreshold.Flagged {
			t.Fatalf("2.1%% divergence must be flagged: %+v", overThreshold)
		}
	})
}
