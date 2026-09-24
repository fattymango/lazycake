package clock

import (
	"sync"
	"time"
)

// Fake is a Clock tests can advance deterministically instead of sleeping.
// Safe for concurrent Now/Advance calls - e.g. a background goroutine (a
// lease.Watcher under test) polling Now() while the test itself calls
// Advance() from a different goroutine.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake starting at t.
func NewFake(t time.Time) *Fake {
	return &Fake{now: t}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
