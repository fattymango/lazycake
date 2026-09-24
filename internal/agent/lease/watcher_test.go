package lease

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mkassab215/lazycake/internal/clock"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWatcherFencesOncePastDeadline(t *testing.T) {
	fc := clock.NewFake(time.Unix(0, 0))
	var deadline atomic.Value
	deadline.Store(fc.Now().Add(10 * time.Second))

	var fences atomic.Int32
	w := &Watcher{
		Deadline:      func() time.Time { return deadline.Load().(time.Time) },
		Clock:         fc,
		Log:           discardLog(),
		CheckInterval: time.Millisecond,
		OnFence:       func() { fences.Add(1) },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	// Not yet past the deadline: give the watcher a few real-time ticks to
	// prove it stays quiet, then advance the fake clock past it.
	time.Sleep(20 * time.Millisecond)
	if fences.Load() != 0 {
		t.Fatalf("fenced before the deadline passed: %d", fences.Load())
	}

	fc.Advance(11 * time.Second)
	time.Sleep(20 * time.Millisecond)
	if fences.Load() != 1 {
		t.Fatalf("expected exactly 1 fence, got %d", fences.Load())
	}

	// Further ticks past the same deadline must not fire again.
	time.Sleep(20 * time.Millisecond)
	if fences.Load() != 1 {
		t.Fatalf("expected no additional fence, got %d", fences.Load())
	}
}

func TestWatcherResetsAfterReconnectExtendsDeadline(t *testing.T) {
	fc := clock.NewFake(time.Unix(0, 0))
	var deadline atomic.Value
	deadline.Store(fc.Now().Add(5 * time.Second))

	var fences atomic.Int32
	w := &Watcher{
		Deadline:      func() time.Time { return deadline.Load().(time.Time) },
		Clock:         fc,
		Log:           discardLog(),
		CheckInterval: time.Millisecond,
		OnFence:       func() { fences.Add(1) },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	fc.Advance(6 * time.Second) // cross the first deadline
	time.Sleep(20 * time.Millisecond)
	if fences.Load() != 1 {
		t.Fatalf("expected 1 fence after first deadline, got %d", fences.Load())
	}

	// Simulate a reconnect: the deadline moves back into the future.
	deadline.Store(fc.Now().Add(10 * time.Second))
	time.Sleep(20 * time.Millisecond)
	if fences.Load() != 1 {
		t.Fatalf("fenced again while within the extended deadline: %d", fences.Load())
	}

	fc.Advance(11 * time.Second) // cross the new deadline too
	time.Sleep(20 * time.Millisecond)
	if fences.Load() != 2 {
		t.Fatalf("expected a second fence after the new deadline passed, got %d", fences.Load())
	}
}

func TestWatcherNeverFencesOnZeroDeadline(t *testing.T) {
	fc := clock.NewFake(time.Unix(0, 0))
	var fences atomic.Int32
	w := &Watcher{
		Deadline:      func() time.Time { return time.Time{} },
		Clock:         fc,
		Log:           discardLog(),
		CheckInterval: time.Millisecond,
		OnFence:       func() { fences.Add(1) },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	fc.Advance(1000 * time.Hour)
	time.Sleep(20 * time.Millisecond)
	if fences.Load() != 0 {
		t.Fatalf("expected no fence with a zero deadline, got %d", fences.Load())
	}
}
