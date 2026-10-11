// Package ratelimit holds the coordinator's per-client throttles: a request-rate limiter and a failed-authentication
// guard, both keyed by the client's IP address. State lives in memory (a restart forgets it, like the login lockout) and
// is pruned as it ages, so a flood of distinct addresses cannot grow it without bound.
package ratelimit

import (
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ClientIP is the host part of a remote address. Deliberately never read from X-Forwarded-For: the coordinator is
// reached directly, and a header the client controls would let it pick its own bucket.
func ClientIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// maxKeys bounds each map; beyond it an unknown key is treated as over the limit rather than stored.
const maxKeys = 100_000

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

// Limiter is a token bucket per key: `burst` requests at once, refilled at `per`/`n` of a second.
type Limiter struct {
	mu        sync.Mutex
	rate      rate.Limit
	burst     int
	buckets   map[string]*bucket
	lastPrune time.Time
	now       func() time.Time
}

// NewLimiter allows `events` per `per` on average, with bursts of up to `burst`.
func NewLimiter(events int, per time.Duration, burst int) *Limiter {
	return &Limiter{
		rate: rate.Every(per / time.Duration(events)), burst: burst,
		buckets: map[string]*bucket{}, now: time.Now,
	}
}

// Allow takes one token for key and reports whether there was one.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastPrune) > time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.seen) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastPrune = now
	}
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxKeys {
			return false
		}
		b = &bucket{lim: rate.NewLimiter(l.rate, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	return b.lim.AllowN(now, 1)
}

type failures struct {
	count        int
	windowStart  time.Time
	blockedUntil time.Time
}

// FailGuard blocks a key for `block` once it has failed authentication `max` times within `window`.
type FailGuard struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	block  time.Duration
	m      map[string]*failures
	now    func() time.Time
}

func NewFailGuard(max int, window, block time.Duration) *FailGuard {
	return &FailGuard{max: max, window: window, block: block, m: map[string]*failures{}, now: time.Now}
}

// Blocked reports whether key is currently blocked.
func (g *FailGuard) Blocked(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	f, ok := g.m[key]
	return ok && g.now().Before(f.blockedUntil)
}

// Fail records one failed authentication for key.
func (g *FailGuard) Fail(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if len(g.m) >= maxKeys {
		for k, f := range g.m {
			if now.After(f.blockedUntil) && now.Sub(f.windowStart) > g.window {
				delete(g.m, k)
			}
		}
		if len(g.m) >= maxKeys {
			return
		}
	}
	f, ok := g.m[key]
	if !ok || now.Sub(f.windowStart) > g.window {
		f = &failures{windowStart: now}
		g.m[key] = f
	}
	f.count++
	if f.count >= g.max {
		f.blockedUntil = now.Add(g.block)
	}
}
