// Package clock is the DI seam for "now": production code takes a Clock
// instead of calling time.Now() directly, so lease/timeout tests can inject
// a fake and advance time deterministically instead of sleeping.
package clock

import "time"

// Clock returns the current time. Real in production, fake in tests.
type Clock interface {
	Now() time.Time
}

// Real is the production Clock, backed by time.Now.
type Real struct{}

func (Real) Now() time.Time { return time.Now() }
