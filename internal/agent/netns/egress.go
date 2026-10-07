package netns

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"

	"golang.org/x/time/rate"
)

// egressLimiter enforces one task's egress_mb cap (PLAN.md: "Exceeding
// [a limit]... egress_mb" appendix reason "egress_exceeded") across every
// connection that task's proxy opens, not just one at a time - a task
// could have up to three gateways, and the cap is per task, not per
// target. Once the cumulative byte count crosses the cap, every open
// connection is closed and OnExceeded fires exactly once.
type egressLimiter struct {
	// CapBytes is the limit; <= 0 means unlimited.
	CapBytes int64
	// OnExceeded, if set, fires exactly once when the cap is crossed.
	OnExceeded func()

	total    atomic.Int64
	exceeded atomic.Bool
	mu       sync.Mutex
	conns    map[net.Conn]struct{}
}

func newEgressLimiter(capBytes int64, onExceeded func()) *egressLimiter {
	return &egressLimiter{CapBytes: capBytes, OnExceeded: onExceeded, conns: map[net.Conn]struct{}{}}
}

func (l *egressLimiter) register(c net.Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.conns[c] = struct{}{}
}

func (l *egressLimiter) unregister(c net.Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.conns, c)
}

// add records n more egress bytes and closes every registered connection
// the first time the running total crosses CapBytes.
func (l *egressLimiter) add(n int) {
	if l.CapBytes <= 0 {
		return
	}
	if l.total.Add(int64(n)) <= l.CapBytes {
		return
	}
	if !l.exceeded.CompareAndSwap(false, true) {
		return // another goroutine already tripped it
	}
	l.mu.Lock()
	for c := range l.conns {
		c.Close()
	}
	l.mu.Unlock()
	if l.OnExceeded != nil {
		l.OnExceeded()
	}
}

// countingWriter wraps an io.Writer, reporting every successful write's
// size to an egressLimiter before returning.
type countingWriter struct {
	w       io.Writer
	limiter *egressLimiter
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		c.limiter.add(n)
	}
	return n, err
}

// TunnelCounters is how many bytes one task has moved through its tunnel since it
// started, across every connection: ToGateway leaves the container, ToTask goes in.
// Read by the usage sampler (display only; the egress cap uses its own count).
type TunnelCounters struct {
	ToGateway atomic.Int64
	ToTask    atomic.Int64
}

// meterWriter adds every successful write's size to a counter.
type meterWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (m meterWriter) Write(p []byte) (int, error) {
	n, err := m.w.Write(p)
	if n > 0 {
		m.n.Add(int64(n))
	}
	return n, err
}

// NewBandwidthLimiter builds the limiter that enforces a machine's offered network bandwidth: one
// shared by every tunnel connection of every task on the agent, counting both directions together.
// mbps <= 0 means no limit (nil).
func NewBandwidthLimiter(mbps int) *rate.Limiter {
	if mbps <= 0 {
		return nil
	}
	perSecond := mbps * 1_000_000 / 8
	burst := perSecond / 10 // a tenth of a second of traffic
	if burst < 64*1024 {
		burst = 64 * 1024
	}
	return rate.NewLimiter(rate.Limit(perSecond), burst)
}

// limitedWriter slows writes to the limiter's rate. A nil limiter passes everything straight through.
type limitedWriter struct {
	ctx context.Context
	w   io.Writer
	lim *rate.Limiter
}

func (l limitedWriter) Write(p []byte) (int, error) {
	if l.lim == nil {
		return l.w.Write(p)
	}
	written := 0
	for written < len(p) {
		chunk := p[written:]
		if max := l.lim.Burst(); len(chunk) > max {
			chunk = chunk[:max]
		}
		if err := l.lim.WaitN(l.ctx, len(chunk)); err != nil {
			return written, err
		}
		n, err := l.w.Write(chunk)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}
