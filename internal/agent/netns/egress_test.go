package netns

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeConn is a minimal net.Conn whose Close is observable, for testing
// egressLimiter without real sockets.
type fakeConn struct {
	net.Conn
	closed atomic.Bool
}

func (f *fakeConn) Close() error {
	f.closed.Store(true)
	return nil
}

func TestEgressLimiterUnlimitedNeverTrips(t *testing.T) {
	var fired atomic.Bool
	l := newEgressLimiter(0, func() { fired.Store(true) })
	l.add(1 << 30)
	if fired.Load() {
		t.Fatal("expected no trip with CapBytes <= 0")
	}
}

func TestEgressLimiterTripsOnceOverCap(t *testing.T) {
	var fired atomic.Int32
	l := newEgressLimiter(100, func() { fired.Add(1) })

	c1, c2 := &fakeConn{}, &fakeConn{}
	l.register(c1)
	l.register(c2)

	l.add(50)
	if fired.Load() != 0 {
		t.Fatal("should not trip before crossing the cap")
	}
	if c1.closed.Load() || c2.closed.Load() {
		t.Fatal("connections closed before cap crossed")
	}

	l.add(60) // total 110 > 100
	if fired.Load() != 1 {
		t.Fatalf("expected OnExceeded to fire exactly once, fired %d times", fired.Load())
	}
	if !c1.closed.Load() || !c2.closed.Load() {
		t.Fatal("expected every registered connection to be closed")
	}

	l.add(1) // further adds must not fire again
	if fired.Load() != 1 {
		t.Fatal("OnExceeded fired more than once")
	}
}

func TestEgressLimiterConcurrentAddsFireExactlyOnce(t *testing.T) {
	var fired atomic.Int32
	l := newEgressLimiter(1000, func() { fired.Add(1) })

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.add(100) // 50 * 100 = 5000, well over the 1000 cap
		}()
	}
	wg.Wait()

	if fired.Load() != 1 {
		t.Fatalf("expected exactly 1 trip under concurrent adds, got %d", fired.Load())
	}
}

func TestCountingWriterReportsBytes(t *testing.T) {
	var buf sliceWriter
	l := newEgressLimiter(1000, nil)
	cw := countingWriter{w: &buf, limiter: l}

	n, err := cw.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("got n=%d, want 5", n)
	}
	if l.total.Load() != 5 {
		t.Fatalf("got total=%d, want 5", l.total.Load())
	}
}

type sliceWriter struct{ data []byte }

func (s *sliceWriter) Write(p []byte) (int, error) {
	s.data = append(s.data, p...)
	return len(p), nil
}
