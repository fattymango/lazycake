package ratelimit

import (
	"testing"
	"time"
)

func TestClientIPIgnoresThePort(t *testing.T) {
	for in, want := range map[string]string{"203.0.113.9:5555": "203.0.113.9", "[2001:db8::1]:443": "2001:db8::1", "weird": "weird"} {
		if got := ClientIP(in); got != want {
			t.Errorf("ClientIP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLimiterAllowsBurstThenThrottlesPerKeyAndRefills(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLimiter(10, time.Minute, 3)
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("request %d inside the burst was refused", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("a 4th immediate request must be refused")
	}
	if !l.Allow("b") {
		t.Fatal("another address has its own bucket")
	}
	now = now.Add(7 * time.Second) // 10/min = one token per 6 s
	if !l.Allow("a") {
		t.Fatal("a token should have refilled")
	}
	if l.Allow("a") {
		t.Fatal("only one token refilled")
	}
}

func TestLimiterForgetsIdleKeys(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLimiter(1, time.Second, 1)
	l.now = func() time.Time { return now }
	l.Allow("old")
	now = now.Add(20 * time.Minute)
	l.Allow("new")
	if _, ok := l.buckets["old"]; ok {
		t.Fatal("an idle key was never pruned")
	}
}

func TestFailGuardBlocksAfterMaxFailuresThenExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewFailGuard(3, time.Minute, 5*time.Minute)
	g.now = func() time.Time { return now }
	g.Fail("ip")
	g.Fail("ip")
	if g.Blocked("ip") {
		t.Fatal("blocked one failure early")
	}
	g.Fail("ip")
	if !g.Blocked("ip") {
		t.Fatal("not blocked after 3 failures")
	}
	if g.Blocked("other") {
		t.Fatal("blocking is per key")
	}
	now = now.Add(6 * time.Minute)
	if g.Blocked("ip") {
		t.Fatal("the block never expired")
	}
}

func TestFailGuardCountsOnlyWithinTheWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewFailGuard(3, time.Minute, time.Hour)
	g.now = func() time.Time { return now }
	g.Fail("ip")
	g.Fail("ip")
	now = now.Add(2 * time.Minute)
	g.Fail("ip")
	if g.Blocked("ip") {
		t.Fatal("failures spread over more than the window must not add up")
	}
}
