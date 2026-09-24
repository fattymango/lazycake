package clock

import "time"

// Fake is a Clock tests can advance deterministically instead of sleeping.
// Not safe for concurrent Advance/Now calls without external locking beyond
// what's needed for simple sequential test use.
type Fake struct {
	now time.Time
}

// NewFake returns a Fake starting at t.
func NewFake(t time.Time) *Fake {
	return &Fake{now: t}
}

func (f *Fake) Now() time.Time { return f.now }

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) { f.now = f.now.Add(d) }
