package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func shrinkBackoff(t *testing.T) {
	t.Helper()
	min, max, healthy := reconnectMin, reconnectMax, healthyAfter
	reconnectMin, reconnectMax, healthyAfter = 5*time.Millisecond, 20*time.Millisecond, time.Hour
	t.Cleanup(func() { reconnectMin, reconnectMax, healthyAfter = min, max, healthy })
}

// A gateway whose relay connection keeps failing must keep retrying rather
// than exit, and must stop promptly (returning nil) once ctx is cancelled.
func TestServeForeverRetriesUntilCancelled(t *testing.T) {
	shrinkBackoff(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- serveForever(ctx, quietLog(), func(ctx context.Context) error {
			if calls.Add(1) >= 4 {
				cancel() // the 4th attempt is where the test stops it
			}
			return errors.New("relay went away")
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveForever returned %v, want nil on cancel", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveForever never returned after ctx was cancelled")
	}
	if got := calls.Load(); got < 4 {
		t.Fatalf("serve called %d times, want >= 4 (it gave up retrying)", got)
	}
}

// Backoff grows between consecutive failures but never past reconnectMax.
func TestServeForeverBackoffIsCapped(t *testing.T) {
	shrinkBackoff(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var times []time.Time
	_ = serveForever(ctx, quietLog(), func(ctx context.Context) error {
		times = append(times, time.Now())
		if len(times) >= 8 {
			cancel()
		}
		return errors.New("down")
	})
	if len(times) < 8 {
		t.Fatalf("only %d attempts", len(times))
	}
	// 5,10,20,20,20,... ms: the last gaps must not exceed the cap by much.
	for i := 5; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap > 200*time.Millisecond {
			t.Fatalf("gap %d = %v, far past the %v cap", i, gap, reconnectMax)
		}
	}
}
