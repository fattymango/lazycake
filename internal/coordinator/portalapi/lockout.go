package portalapi

import (
	"sync"
	"time"
)

// loginLimiter is task 7.2's "basic brute-force protection" (PLAN.md §2):
// a per-username failed-attempt counter that briefly locks out after 10
// failures in a row within a few minutes. An in-memory map is exactly what
// the plan calls for at this scale - it isn't meant to survive a
// coordinator restart or work across multiple replicas, same documented
// scope as OPEN_QUESTIONS.md's "left for the implementer" note on this
// task.
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*attemptRecord
}

type attemptRecord struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

const (
	maxFailedAttempts = 10
	failureWindow     = 5 * time.Minute
	lockoutDuration   = 5 * time.Minute
)

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: map[string]*attemptRecord{}}
}

// Locked reports whether username is currently locked out.
func (l *loginLimiter) Locked(username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.attempts[username]
	if !ok {
		return false
	}
	return time.Now().Before(rec.lockedUntil)
}

// RecordFailure counts one more failed attempt for username, locking it
// out once maxFailedAttempts is reached within failureWindow. A failure
// outside the window starts a fresh window rather than accumulating
// forever.
func (l *loginLimiter) RecordFailure(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	rec, ok := l.attempts[username]
	if !ok || now.Sub(rec.windowStart) > failureWindow {
		rec = &attemptRecord{windowStart: now}
		l.attempts[username] = rec
	}
	rec.count++
	if rec.count >= maxFailedAttempts {
		rec.lockedUntil = now.Add(lockoutDuration)
	}
}

// RecordSuccess clears any tracked failures for username.
func (l *loginLimiter) RecordSuccess(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, username)
}
