package conn

import (
	"math/rand/v2"
	"time"
)

// Backoff computes reconnect delays: doubling from 1s, capped at 60s, with
// +/-20% jitter so many agents restarted together don't reconnect in lockstep.
type Backoff struct {
	Min, Max time.Duration
	attempt  int
	rand     func() float64 // seam for deterministic tests; defaults to rand/v2
}

// NewBackoff returns a Backoff starting at 1s, capped at 60s, per
// IMPLEMENTATION.md task 1.4.
func NewBackoff() *Backoff {
	return &Backoff{Min: time.Second, Max: 60 * time.Second}
}

// Next returns the delay before the next attempt and advances state.
func (b *Backoff) Next() time.Duration {
	base := b.Min << b.attempt
	if base <= 0 || base > b.Max { // overflow or past the cap
		base = b.Max
	} else {
		b.attempt++
	}
	f := rand.Float64
	if b.rand != nil {
		f = b.rand
	}
	jitter := 1 + (f()*2-1)*0.2 // uniform in [0.8, 1.2]
	d := time.Duration(float64(base) * jitter)
	if d < 0 {
		d = base
	}
	return d
}

// Reset returns Backoff to its initial state after a successful connection.
func (b *Backoff) Reset() { b.attempt = 0 }
