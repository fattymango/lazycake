package exec

import (
	"strings"
	"testing"
	"time"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func newTestRelay() (*logRelay, *[]*lazycakev1.LogBatch) {
	var sent []*lazycakev1.LogBatch
	r := &logRelay{
		taskID:      "tsk_1",
		windowStart: time.Now(),
		send: func(m *lazycakev1.AgentMessage) {
			sent = append(sent, m.GetLogs())
		},
	}
	return r, &sent
}

func TestLogRelayFlushesOnByteThreshold(t *testing.T) {
	r, sent := newTestRelay()
	big := strings.Repeat("x", logFlushBytes) // one line at exactly the threshold
	r.add(big)
	if len(*sent) != 1 {
		t.Fatalf("expected 1 flush at the byte threshold, got %d", len(*sent))
	}
}

func TestLogRelayBuffersUntilFlush(t *testing.T) {
	r, sent := newTestRelay()
	r.add("hello")
	if len(*sent) != 0 {
		t.Fatalf("expected no flush yet, got %d", len(*sent))
	}
	r.flush()
	if len(*sent) != 1 || len((*sent)[0].GetLines()) != 1 {
		t.Fatalf("expected exactly one batch with one line, got %+v", *sent)
	}
}

func TestLogRelayTotalCapTruncates(t *testing.T) {
	r, sent := newTestRelay()
	// Simulate having already relayed right up to the cap (reaching it for
	// real would take 50 seconds at the 1MB/s rate limit) rather than
	// looping millions of adds through the limiter.
	r.totalBytes = logTotalCap - 10
	r.add(strings.Repeat("y", 1024))
	r.flush()

	if !r.truncated {
		t.Fatal("expected relay to mark itself truncated")
	}

	// The truncation marker must appear exactly once, as the last line sent.
	var lastLine string
	for _, batch := range *sent {
		for _, l := range batch.GetLines() {
			lastLine = l.GetLine()
		}
	}
	if lastLine != truncationLine {
		t.Fatalf("expected last line to be the truncation marker, got %q", lastLine)
	}

	// Further adds after truncation must be no-ops.
	before := len(*sent)
	r.add("should be dropped")
	r.flush()
	if len(*sent) != before {
		t.Fatalf("expected no further sends after truncation, got %d new batches", len(*sent)-before)
	}
}

func TestLogRelayRateLimitsWithinWindow(t *testing.T) {
	r, sent := newTestRelay()
	line := strings.Repeat("z", 1024) // 1KB
	// 1MB/s budget / 1KB lines = 1024 lines fit in one window.
	for i := 0; i < 2000; i++ {
		r.add(line)
	}
	r.flush()

	var total int64
	for _, batch := range *sent {
		for _, l := range batch.GetLines() {
			total += int64(len(l.GetLine()))
		}
	}
	if total > logRateLimit {
		t.Fatalf("sent %d bytes in one window, want <= %d (rate limit)", total, logRateLimit)
	}
	if total == 0 {
		t.Fatal("expected some lines to get through before the rate limit kicked in")
	}
}
