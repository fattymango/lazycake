package listener

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestNewBandwidthLimiterUnlimited(t *testing.T) {
	if NewBandwidthLimiter(0) != nil {
		t.Fatal("0 Mbps must mean no limiter")
	}
}

// 8 Mbps = 1 MB/s; the limiter starts with a 100 ms burst (but at least 64 KiB), so writing
// 300 KB must take well over 200 ms, while an unlimited writer is immediate.
func TestLimitedWriterSlowsWrites(t *testing.T) {
	data := make([]byte, 300_000)
	var out bytes.Buffer
	w := limitedWriter{ctx: context.Background(), w: &out, lim: NewBandwidthLimiter(8)}
	start := time.Now()
	if n, err := w.Write(data); err != nil || n != len(data) {
		t.Fatalf("write: %d %v", n, err)
	}
	if took := time.Since(start); took < 150*time.Millisecond {
		t.Fatalf("limited write finished in %v, want it throttled", took)
	}
	if out.Len() != len(data) {
		t.Fatalf("lost bytes: %d", out.Len())
	}

	out.Reset()
	start = time.Now()
	(limitedWriter{ctx: context.Background(), w: &out}).Write(data)
	if took := time.Since(start); took > 50*time.Millisecond {
		t.Fatalf("unlimited write took %v", took)
	}
}
