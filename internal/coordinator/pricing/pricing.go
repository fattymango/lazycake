// Package pricing implements IMPLEMENTATION.md task 4.4's price formula in
// isolation from billing's store/ledger machinery, so task 4.5's
// submission-time affordability check (internal/coordinator/api) can use
// the exact same formula billing.Ledger settles with, without api and
// billing importing each other (billing already depends on api for
// TaskFinishedEvent).
package pricing

import (
	"math"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// Rates are the coordinator's pricing knobs:
//
//	price_micros = base_fee
//	             + (cpu_rate * cores + ram_rate * memory_gb + disk_rate * disk_gb) * normalised_s
//	             + net_rate * bytes / 1e9
//	             + cold_start_fee_if_pulled
type Rates struct {
	BaseFeeMicros      int64
	CPURateMicros      int64 // per core-second
	RAMRateMicros      int64 // per GB-second
	DiskRateMicros     int64 // per GB-second
	NetRateMicros      int64 // per GB transferred
	ColdStartFeeMicros int64
}

// DefaultRates are placeholder numbers - real pricing is a business
// decision outside this codebase's scope, not something to invent here.
// Chosen so a typical small task (1 core, 512MB, 1GB disk, ~10s
// normalised, no network, no cold pull) costs a few thousand micros
// (fractions of a cent), the right order of magnitude for a
// leftover-CPU marketplace without claiming to be an actual price list.
func DefaultRates() Rates {
	return Rates{
		BaseFeeMicros:      100,   // $0.0001 per task
		CPURateMicros:      50,    // $0.00005 per core-second
		RAMRateMicros:      10,    // $0.00001 per GB-second
		DiskRateMicros:     2,     // $0.000002 per GB-second
		NetRateMicros:      1_000, // $0.001 per GB
		ColdStartFeeMicros: 5_000, // $0.005 per cold pull
	}
}

// Price computes price_micros for one task run at the given limits,
// normalised duration, network bytes, and whether a cold pull applies.
func Price(rates Rates, limits store.Limits, normalisedS float64, bytesNet int64, coldPull bool) int64 {
	ramGB := float64(limits.MemoryMB) / 1024
	diskGB := float64(limits.DiskMB) / 1024
	perSecond := float64(rates.CPURateMicros)*limits.CPUCores +
		float64(rates.RAMRateMicros)*ramGB +
		float64(rates.DiskRateMicros)*diskGB

	price := float64(rates.BaseFeeMicros) + perSecond*normalisedS + float64(rates.NetRateMicros)*(float64(bytesNet)/1e9)
	if coldPull {
		price += float64(rates.ColdStartFeeMicros)
	}
	if price < 0 {
		price = 0
	}
	return int64(math.Round(price))
}

// WorstCase bounds task 4.5's submission-time affordability check: the
// price if the task ran for its full wall_timeout_s (the real protection
// against a runaway task is max_duration killing it and stopping billing -
// PLAN.md "Payout rules" - so wall_timeout_s is the true upper bound on
// billable duration) at bench_score 1.0 (the reference rate - a node
// faster than reference is a placement/spec-drift concern, task 5.1, not
// something a customer should be blocked from affording up front), using
// the task's own declared egress cap as the worst-case network transfer,
// and assuming the pessimistic case of a cold pull.
func WorstCase(rates Rates, limits store.Limits) int64 {
	worstBytes := int64(limits.EgressMB) << 20
	return Price(rates, limits, float64(limits.WallTimeoutS), worstBytes, true)
}
