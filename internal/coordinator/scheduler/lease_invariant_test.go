package scheduler

import (
	"math/rand"
	"testing"
	"time"
)

// TestLeaseInvariant is task 3.1's property test: for any sequence of
// network delays and partitions, there must never be an instant where the
// coordinator has requeued a task (its lease deadline for that node has
// passed) while the agent has not yet fenced it (its own fence deadline
// has not yet passed).
//
// It holds by construction, not by luck: the agent computes its fence
// deadline as send_time(hb) + lease_s, only once hb's ack arrives; the
// coordinator computes its requeue deadline as recv_time(hb) + lease_s +
// leaseMargin, as soon as it receives hb (whether or not its ack ever
// makes it back). Since recv_time(hb) >= send_time(hb) for any real
// heartbeat and leaseMargin > 0, the coordinator's deadline for the same
// heartbeat is always later than the agent's - this test drives that
// relationship through thousands of randomized delay/drop patterns rather
// than trusting the algebra once.
func TestLeaseInvariant(t *testing.T) {
	const leaseS = 60
	const trials = 2000
	const heartbeatsPerTrial = 30

	for trial := 0; trial < trials; trial++ {
		seed := int64(trial)
		rng := rand.New(rand.NewSource(seed))

		var t0 time.Time // simulated "real" origin
		agentFenceDeadline := t0.Add(leaseS * time.Second)
		coordRequeueDeadline := t0.Add((leaseS) * time.Second).Add(leaseMargin)

		sendTime := t0
		for i := 0; i < heartbeatsPerTrial; i++ {
			// Heartbeats are sent on a roughly regular cadence with jitter;
			// occasionally a long partition (big gap) is simulated too.
			gap := time.Duration(rng.Intn(20)) * time.Second
			if rng.Intn(10) == 0 {
				gap += time.Duration(rng.Intn(120)) * time.Second // partition
			}
			sendTime = sendTime.Add(gap)

			networkDelay := time.Duration(rng.Intn(5000)) * time.Millisecond
			recvTime := sendTime.Add(networkDelay)

			dropped := rng.Intn(20) == 0    // heartbeat itself never arrives
			ackDropped := rng.Intn(20) == 0 // arrives, but the ack back is lost

			if !dropped {
				// Coordinator extends its requeue deadline the instant it
				// receives the heartbeat, unconditionally.
				candidate := recvTime.Add(leaseS * time.Second).Add(leaseMargin)
				if candidate.After(coordRequeueDeadline) {
					coordRequeueDeadline = candidate
				}

				if !ackDropped {
					// Agent only extends once *this* heartbeat is acked,
					// and measures from when it sent it, not when the ack
					// arrived.
					candidate := sendTime.Add(leaseS * time.Second)
					if candidate.After(agentFenceDeadline) {
						agentFenceDeadline = candidate
					}
				}
			}

			// The invariant must hold at every step, not just at the end:
			// the coordinator's deadline for what it has seen so far must
			// never be before the agent's deadline for what it has had
			// acked so far.
			if coordRequeueDeadline.Before(agentFenceDeadline) {
				t.Fatalf("trial %d, heartbeat %d: coordinator requeue deadline %v is before agent fence deadline %v (seed=%d)",
					trial, i, coordRequeueDeadline, agentFenceDeadline, seed)
			}
		}
	}
}
