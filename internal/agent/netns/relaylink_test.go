package netns

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRelayConn struct {
	name   string
	dead   atomic.Bool
	closed atomic.Bool
	// gate, when set, holds every failing OpenStream until it is closed, so a test can have many callers inside the dead connection at once.
	gate chan struct{}
}

type nopStream struct{ io.ReadWriteCloser }

func (f *fakeRelayConn) OpenStream(context.Context, string, string) (io.ReadWriteCloser, error) {
	if f.dead.Load() {
		if f.gate != nil {
			<-f.gate
		}
		return nil, errors.New("timeout: no recent network activity")
	}
	return nopStream{}, nil
}
func (f *fakeRelayConn) Closed() bool { return f.dead.Load() }
func (f *fakeRelayConn) Close()       { f.closed.Store(true) }

func fastDial(t *testing.T) {
	t.Helper()
	a, d := dialAttempts, dialRetryDelay
	dialAttempts, dialRetryDelay = 4, time.Millisecond
	t.Cleanup(func() { dialAttempts, dialRetryDelay = a, d })
}

// A single lost packet at the start used to fail the whole task with no output.
func TestFirstDialIsRetried(t *testing.T) {
	fastDial(t)
	var calls atomic.Int32
	l, err := newRelayLink(context.Background(), func(context.Context) (relayConn, error) {
		if calls.Add(1) < 3 {
			return nil, errors.New("timeout: no recent network activity")
		}
		return &fakeRelayConn{}, nil
	}, nil)
	if err != nil || l == nil {
		t.Fatalf("the third attempt works, so the link must come up: %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("dialed %d times, want 3", calls.Load())
	}
}

func TestFirstDialGivesUpAfterTheLastAttempt(t *testing.T) {
	fastDial(t)
	var calls atomic.Int32
	_, err := newRelayLink(context.Background(), func(context.Context) (relayConn, error) {
		calls.Add(1)
		return nil, errors.New("down")
	}, nil)
	if err == nil {
		t.Fatal("a relay that never answers must fail the link")
	}
	if calls.Load() != 4 {
		t.Fatalf("tried %d times, want exactly the 4 attempts", calls.Load())
	}
}

// The relay connection died (idle timeout, lost UDP path): the next stream must go out on a fresh one, and a crowd of connections that all notice
// at once must cause one redial, not one each.
func TestStreamsGoOverAFreshConnectionOnceTheOldOneIsDead(t *testing.T) {
	fastDial(t)
	first := &fakeRelayConn{name: "first"}
	var dials atomic.Int32
	fresh := &fakeRelayConn{name: "fresh"}
	l, err := newRelayLink(context.Background(), func(context.Context) (relayConn, error) {
		if dials.Add(1) == 1 {
			return first, nil
		}
		return fresh, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.OpenStream(context.Background(), "gw", "tsk"); err != nil {
		t.Fatalf("a live connection must just work: %v", err)
	}

	first.dead.Store(true)
	first.gate = make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := l.OpenStream(context.Background(), "gw", "tsk")
			errs <- err
		}()
	}
	time.Sleep(100 * time.Millisecond) // all twenty are now inside the dead connection
	close(first.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("every stream must succeed on the fresh connection: %v", err)
		}
	}
	if dials.Load() != 2 {
		t.Fatalf("dialed %d times: twenty connections noticing together must replace the dead one once (2 dials in all)", dials.Load())
	}
	if !first.closed.Load() {
		t.Fatal("the dead connection was not closed")
	}
}

// A refusal that is not a dead connection (the gateway is not there) must not trigger a redial, and a fresh connection that fails too is reported,
// not retried forever.
func TestNoRedialWhenTheConnectionIsAliveAndNoLoopWhenTheFreshOneFailsToo(t *testing.T) {
	fastDial(t)
	var dials atomic.Int32
	live := &fakeRelayConn{}
	l, _ := newRelayLink(context.Background(), func(context.Context) (relayConn, error) { dials.Add(1); return live, nil }, nil)
	// alive but the open fails (not Closed): the error is returned as is
	failing := &refusingConn{}
	l.conn = failing
	if _, err := l.OpenStream(context.Background(), "gw", "tsk"); err == nil {
		t.Fatal("expected the open error to come back")
	}
	if dials.Load() != 1 {
		t.Fatalf("a live connection that refused one stream was redialed (%d dials)", dials.Load())
	}

	dead1, dead2 := &fakeRelayConn{}, &fakeRelayConn{}
	dead1.dead.Store(true)
	dead2.dead.Store(true)
	var n atomic.Int32
	l2, _ := newRelayLink(context.Background(), func(context.Context) (relayConn, error) {
		if n.Add(1) == 1 {
			return dead1, nil
		}
		return dead2, nil
	}, nil)
	if _, err := l2.OpenStream(context.Background(), "gw", "tsk"); err == nil {
		t.Fatal("both connections are dead: the open must fail, once")
	}
	if n.Load() != 2 {
		t.Fatalf("dialed %d times: one redial per failed open, never a loop", n.Load())
	}
}

type refusingConn struct{}

func (refusingConn) OpenStream(context.Context, string, string) (io.ReadWriteCloser, error) {
	return nil, errors.New("gateway not connected")
}
func (refusingConn) Closed() bool { return false }
func (refusingConn) Close()       {}
