// Package listener is the gateway's inbound side: for each stream the
// relay opens toward this gateway, terminate the inner Noise session,
// read which published service the container wants, and forward to it.
// "A gateway only forwards to services in its own published list. Anything
// else is refused." (PLAN.md)
package listener

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"syscall"
	"time"

	quicgo "github.com/quic-go/quic-go"

	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

// ForwardStats is reported once one forwarded connection closes.
type ForwardStats struct {
	TaskID       string
	Service      string
	BytesToLocal int64
	BytesToTask  int64
}

// ForwardReport is what one forwarded connection moved since its previous
// report: a delta, not a running total. A long-lived connection reports every
// progressEvery while it is open (so its traffic shows up over time rather than
// as one spike when it finally closes), and once more, with Final set, when it
// closes. Summing a connection's reports gives its totals.
type ForwardReport struct {
	TaskID  string
	Service string
	ToLocal int64 // gateway -> local service
	ToTask  int64 // local service -> gateway -> task
	Final   bool
}

// progressEvery is how often an open connection reports. A variable so tests
// don't have to wait ten seconds.
var progressEvery = 10 * time.Second

// countingWriter counts bytes as they pass, so a copy that is still running
// can be reported on.
type countingWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (c countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n.Add(int64(n))
	return n, err
}

// Listener accepts relayed streams from one QUIC connection to the relay
// and forwards each to a published local service.
type Listener struct {
	Conn     *quicgo.Conn
	Keypair  noise.Keypair
	Services map[string]string // service name -> "host:port" to dial
	Log      *slog.Logger

	// Dial opens the local service connection; defaults to net.Dial.
	// Overridable so tests don't need a real TCP listener.
	Dial func(network, addr string) (net.Conn, error)

	// OnForward, if set, is called once per forwarded connection after it
	// closes, with final byte counts.
	OnForward func(ForwardStats)

	// OnReport, if set, receives each connection's byte-count deltas while it
	// is open and a final one when it closes (see ForwardReport).
	OnReport func(ForwardReport)
}

func (l *Listener) dial(network, addr string) (net.Conn, error) {
	if l.Dial != nil {
		return l.Dial(network, addr)
	}
	return net.Dial(network, addr)
}

// Run accepts relayed streams until ctx is cancelled or the connection to
// the relay drops.
func (l *Listener) Run(ctx context.Context) error {
	for {
		stream, taskID, err := quic.AcceptRelayedStream(ctx, l.Conn)
		if err != nil {
			return fmt.Errorf("accepting relayed stream: %w", err)
		}
		go l.handleStream(taskID, stream)
	}
}

// localDialTimeout bounds a probe's dial of a local service. It must stay
// below the coordinator's reply timeout (quic.probeReplyTimeout) so a slow
// service shows up as a failed dial, not as a silent gateway.
const localDialTimeout = 2 * time.Second

// handleProbe answers a connectivity probe from the coordinator: dial the named
// service on this machine and say whether it worked. No customer data is
// involved, and the connection is closed straight away.
func (l *Listener) handleProbe(service string, stream *quicgo.Stream) {
	defer stream.Close()
	reply := quic.ProbeReply{}
	addr, ok := l.Services[service]
	if !ok {
		reply.Error = "this gateway doesn't publish a service with that name"
	} else {
		start := time.Now()
		var conn net.Conn
		var err error
		if l.Dial != nil {
			conn, err = l.Dial("tcp", addr)
		} else {
			conn, err = net.DialTimeout("tcp", addr, localDialTimeout)
		}
		reply.Ms = time.Since(start).Milliseconds()
		if err != nil {
			reply.Error = probeDialError(err)
		} else {
			conn.Close()
			reply.OK = true
		}
	}
	if err := quic.WriteProbeReply(stream, reply); err != nil {
		l.Log.Debug("answering a probe", "service", service, "error", err)
	}
}

// probeDialError turns a dial failure into a sentence a person can act on,
// without leaking more than the cause.
func probeDialError(err error) string {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused: nothing is listening on that port on the gateway's machine"
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return "timed out: the service didn't answer in time"
	default:
		return "could not connect: " + err.Error()
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func (l *Listener) handleStream(taskID string, stream *quicgo.Stream) {
	if service, isProbe := quic.ProbeService(taskID); isProbe {
		l.handleProbe(service, stream)
		return
	}
	session, _, err := noise.DoResponderHandshake(stream, l.Keypair)
	if err != nil {
		l.Log.Warn("noise handshake failed", "task_id", taskID, "error", err)
		stream.CancelWrite(1)
		return
	}

	// Convention: the first Noise message the agent's proxy sends is the
	// target service name, exactly (see internal/agent/netns, phase 2.5).
	nameBuf := make([]byte, 256)
	n, err := session.Read(nameBuf)
	if err != nil {
		l.Log.Warn("reading service name", "task_id", taskID, "error", err)
		return
	}
	service := string(nameBuf[:n])

	addr, ok := l.Services[service]
	if !ok {
		l.Log.Warn("refusing unpublished service", "task_id", taskID, "service", service)
		return
	}

	local, err := l.dial("tcp", addr)
	if err != nil {
		l.Log.Warn("dialing local service", "task_id", taskID, "service", service, "error", err)
		return
	}

	stats := ForwardStats{TaskID: taskID, Service: service}
	var toLocal, toTask atomic.Int64
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(countingWriter{w: local, n: &toLocal}, session)
		// Unblock the reverse copy: once the task side has nothing more
		// to send, closing the local connection makes its next Read
		// return so the other goroutine below can finish too.
		local.Close()
		done <- struct{}{}
	}()
	go func() {
		io.Copy(countingWriter{w: session, n: &toTask}, local)
		stream.Close()
		done <- struct{}{}
	}()

	// Report what has moved so far every progressEvery while the connection is
	// open, so a connection that stays up for hours isn't invisible until it ends.
	var reportedLocal, reportedTask int64
	report := func(final bool) {
		cl, ct := toLocal.Load(), toTask.Load()
		dl, dt := cl-reportedLocal, ct-reportedTask
		if !final && dl == 0 && dt == 0 {
			return
		}
		reportedLocal, reportedTask = cl, ct
		if l.OnReport != nil {
			l.OnReport(ForwardReport{TaskID: taskID, Service: service, ToLocal: dl, ToTask: dt, Final: final})
		}
	}
	stopProgress := make(chan struct{})
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		tick := time.NewTicker(progressEvery)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				report(false)
			case <-stopProgress:
				return
			}
		}
	}()
	<-done
	<-done
	close(stopProgress)
	<-progressDone
	report(true)
	stats.BytesToLocal, stats.BytesToTask = toLocal.Load(), toTask.Load()

	l.Log.Info("forwarded connection closed", "task_id", taskID, "service", service,
		"bytes_to_local", stats.BytesToLocal, "bytes_to_task", stats.BytesToTask)
	if l.OnForward != nil {
		l.OnForward(stats)
	}
}
