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
	})

	t.Run("dishonest agent inflating its own byte count is flagged", func(t *testing.T) {
		// The relay and gateway - neither of which the agent controls - both saw ~1MB actually move; the agent claims 3x that.
		rec := Reconcile("tsk_dishonest", "nod_2", 3_000_000, 1_000_000, 1_010_000)
		if !rec.Flagged {
			t.Fatalf("inflated agent report not flagged: %+v", rec)
		}
		if rec.DivergencePct <= 0.02 {
			t.Fatalf("divergence_pct = %v, want > 2%%", rec.DivergencePct)
		}
	})

	t.Run("an agent under-reporting against the gateway is flagged too", func(t *testing.T) {
		rec := Reconcile("tsk_under", "nod_2", 500_000, 1_000_000, 1_000_000)
		if !rec.Flagged {
			t.Fatalf("under-reporting agent not flagged: %+v", rec)
		}
	})

	// The regression found by the production load test: a task making hundreds of tiny connections has a
	// relay count (encrypted, framed) a third larger than the real bytes. Honest machines were banned for it.
	t.Run("the relay's wire overhead on many tiny connections is not fraud", func(t *testing.T) {
		rec := Reconcile("tsk_tiny", "nod_1", 287_980, 442_320, 287_980) // measured: agent == gateway, relay +54%
		if rec.Flagged {
			t.Fatalf("honest agent flagged because the relay counts encryption overhead: %+v", rec)
		}
	})

	t.Run("but a relay that saw far LESS than the endpoints claim is flagged", func(t *testing.T) {
		rec := Reconcile("tsk_ghost", "nod_1", 2_000_000, 400_000, 2_000_000)
		if !rec.Flagged {
			t.Fatalf("endpoints claim 2 MB but the relay carried 0.4 MB: must be flagged: %+v", rec)
		}
	})

	t.Run("a few bytes of close-timing difference on a tiny task is not fraud", func(t *testing.T) {
		rec := Reconcile("tsk_hello", "nod_1", 91, 0, 182) // 50% apart, but 91 bytes
		if rec.Flagged {
			t.Fatalf("a difference of 91 bytes must not flag a node: %+v", rec)
		}
	})

	t.Run("all zero never divides by zero", func(t *testing.T) {
		rec := Reconcile("tsk_notunnel", "nod_1", 0, 0, 0)
		if rec.Flagged {
			t.Fatalf("a task with no tunnel traffic at all must not be flagged: %+v", rec)
		}
	})

	t.Run("exactly at the 2%% threshold is not flagged, just over is", func(t *testing.T) {
		atThreshold := Reconcile("tsk_at", "nod_1", 10_000_000, 9_800_000, 10_000_000) // relay 2.0% short
		if atThreshold.Flagged {
			t.Fatalf("exactly 2%% divergence must not be flagged (threshold is exclusive): %+v", atThreshold)
		}
		overThreshold := Reconcile("tsk_over", "nod_1", 10_000_000, 9_790_000, 10_000_000) // 2.1% short
		if !overThreshold.Flagged {
			t.Fatalf("2.1%% divergence must be flagged: %+v", overThreshold)
		}
	})
}
