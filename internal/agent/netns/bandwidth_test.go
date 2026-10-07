package netns

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

func TestBandwidthLimiterSlowsTrafficToTheOfferedRateWithoutLosingBytes(t *testing.T) {
	// 8 Mbit/s = 1,000,000 bytes/s, with a 100,000-byte burst: 600,000 bytes should take about 0.5 s.
	lim := NewBandwidthLimiter(8)
	var out bytes.Buffer
	w := limitedWriter{ctx: context.Background(), w: &out, lim: lim}
	payload := bytes.Repeat([]byte("x"), 600_000)

	start := time.Now()
	n, err := io.Copy(w, bytes.NewReader(payload))
	elapsed := time.Since(start)
	if err != nil || n != int64(len(payload)) || out.Len() != len(payload) {
		t.Fatalf("copied %d of %d bytes (err %v): the limiter must never drop data", n, len(payload), err)
	}
	if elapsed < 350*time.Millisecond {
		t.Fatalf("600 kB took %v at an 8 Mbit/s limit; it should take roughly 0.5 s", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("took %v: far slower than the offered rate", elapsed)
	}
}

func TestOneLimiterIsSharedByEverythingSoTheLimitIsOnTheMachine(t *testing.T) {
	// Two concurrent "tasks" each send 300,000 bytes through the SAME 8 Mbit/s limiter: together that is
	// 600,000 bytes, so it takes as long as one task sending all of it, not half as long.
	lim := NewBandwidthLimiter(8)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out bytes.Buffer
			_, _ = io.Copy(limitedWriter{ctx: context.Background(), w: &out, lim: lim}, bytes.NewReader(bytes.Repeat([]byte("y"), 300_000)))
		}()
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("two tasks together moved 600 kB in %v: the limit is not shared across tasks", elapsed)
	}
}

func TestNoLimitIsInstantAndACancelledContextStopsAWrite(t *testing.T) {
	if NewBandwidthLimiter(0) != nil {
		t.Fatal("0 Mbps means no limit")
	}
	var out bytes.Buffer
	start := time.Now()
	_, _ = io.Copy(limitedWriter{ctx: context.Background(), w: &out, lim: nil}, bytes.NewReader(bytes.Repeat([]byte("z"), 5_000_000)))
	if time.Since(start) > 500*time.Millisecond || out.Len() != 5_000_000 {
		t.Fatalf("an unlimited writer should pass everything straight through (took %v, wrote %d)", time.Since(start), out.Len())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slow := limitedWriter{ctx: ctx, w: io.Discard, lim: NewBandwidthLimiter(1)}
	if _, err := slow.Write(bytes.Repeat([]byte("q"), 500_000)); err == nil {
		t.Fatal("a cancelled context must stop a write that would have to wait")
	}
}
