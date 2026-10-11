package netns

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	quicgo "github.com/quic-go/quic-go"

	"github.com/fattymango/lazycake/internal/tunnel/quic"
)

// relayConn is one connection to the coordinator's relay, as far as a task's tunnel is concerned.
type relayConn interface {
	// OpenStream opens a stream toward gatewayID for taskID.
	OpenStream(ctx context.Context, gatewayID, taskID string) (io.ReadWriteCloser, error)
	// Closed reports that the connection is gone (it idled out, or the relay dropped it): nothing more can be opened on it.
	Closed() bool
	Close()
}

type quicRelayConn struct{ c *quicgo.Conn }

func (q quicRelayConn) OpenStream(ctx context.Context, gatewayID, taskID string) (io.ReadWriteCloser, error) {
	s, err := quic.OpenRelayedStream(ctx, q.c, gatewayID, taskID)
	if err != nil {
		return nil, err
	}
	return s, nil
}
func (q quicRelayConn) Closed() bool { return q.c.Context().Err() != nil }
func (q quicRelayConn) Close()       { q.c.CloseWithError(0, "replaced by a fresh connection") }

// How a link dials: a few attempts a short while apart, so one lost packet or a busy relay does not fail a task before it starts. Variables so tests
// need not wait.
var (
	dialAttempts   = 4
	dialRetryDelay = 500 * time.Millisecond
)

// relayLink is a task's connection to the relay that mends itself. A task used to dial the relay once when it started and keep that single
// connection for its whole life; if it died (the relay's idle timeout, a lost UDP path under load) every later connection the task made failed on
// the dead one: in the production load test, one task logged 33,000 "opening relayed stream: timeout" errors and another failed at start with no
// output after a single failed dial. Now the first dial is retried, and a stream that cannot be opened because the connection is gone is retried
// once on a fresh connection.
type relayLink struct {
	dial func(ctx context.Context) (relayConn, error)
	log  *slog.Logger

	mu   sync.Mutex
	conn relayConn
}

// newRelayLink dials the relay, retrying a few times.
func newRelayLink(ctx context.Context, dial func(ctx context.Context) (relayConn, error), log *slog.Logger) (*relayLink, error) {
	l := &relayLink{dial: dial, log: log}
	c, err := l.dialWithRetry(ctx)
	if err != nil {
		return nil, err
	}
	l.conn = c
	return l, nil
}

func (l *relayLink) dialWithRetry(ctx context.Context) (relayConn, error) {
	var err error
	for attempt := 1; attempt <= dialAttempts; attempt++ {
		var c relayConn
		if c, err = l.dial(ctx); err == nil {
			return c, nil
		}
		if l.log != nil {
			l.log.Warn("dialing relay", "attempt", attempt, "of", dialAttempts, "error", err)
		}
		if attempt == dialAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * dialRetryDelay):
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", dialAttempts, err)
}

func (l *relayLink) current() relayConn {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.conn
}

// replace swaps a dead connection for a fresh one, once however many connections noticed it at the same time.
func (l *relayLink) replace(ctx context.Context, dead relayConn) (relayConn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn != dead {
		return l.conn, nil // someone else already did
	}
	c, err := l.dialWithRetry(ctx)
	if err != nil {
		return nil, err
	}
	l.conn = c
	dead.Close()
	if l.log != nil {
		l.log.Info("relay connection was lost; reconnected")
	}
	return c, nil
}

// OpenStream opens a stream toward gatewayID for taskID, reconnecting once if the connection turns out to be dead.
func (l *relayLink) OpenStream(ctx context.Context, gatewayID, taskID string) (io.ReadWriteCloser, error) {
	c := l.current()
	s, err := c.OpenStream(ctx, gatewayID, taskID)
	if err == nil || !c.Closed() {
		return s, err
	}
	fresh, derr := l.replace(ctx, c)
	if derr != nil {
		return nil, fmt.Errorf("%w (and reconnecting failed: %v)", err, derr)
	}
	return fresh.OpenStream(ctx, gatewayID, taskID)
}
