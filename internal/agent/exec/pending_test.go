package exec

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	lazycakev1 "github.com/fattymango/lazycake/internal/proto/lazycake/v1"
)

// linkSender is a connection that is either up or down, as *conn.Runner is: while it is down a message has nowhere to go and
// TrySend says so.
type linkSender struct {
	mu   sync.Mutex
	up   bool
	sent []*lazycakev1.AgentMessage
}

func (l *linkSender) setUp(up bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.up = up
}

func (l *linkSender) Send(m *lazycakev1.AgentMessage) { l.TrySend(m) }

func (l *linkSender) TrySend(m *lazycakev1.AgentMessage) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.up {
		return false
	}
	l.sent = append(l.sent, m)
	return true
}

func (l *linkSender) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.sent)
}

// plainSender is a Sender that has no way to say whether a message went out.
type plainSender struct {
	mu   sync.Mutex
	sent []*lazycakev1.AgentMessage
}

func (p *plainSender) Send(m *lazycakev1.AgentMessage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, m)
}

func newReportingExecutor(s Sender) *Executor {
	return &Executor{Send: s, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestAFinishedReportMadeWithNoConnectionIsSentWhenTheAgentRegisters(t *testing.T) {
	link := &linkSender{}
	e := newReportingExecutor(link)

	e.reportFinished("tsk_1", finished("tsk_1", -1, "fenced", time.Now()))
	require.Equal(t, 0, link.count(), "there was no connection to send it on")

	link.setUp(true)
	e.ReplayPending()
	require.Equal(t, 1, link.count())
	require.Equal(t, "fenced", link.sent[0].GetFinished().GetExitReason())

	e.ReplayPending()
	require.Equal(t, 1, link.count(), "a report that went out is not sent again at the next registration")
}

func TestAFinishedReportThatWentOutIsNotSentAgain(t *testing.T) {
	link := &linkSender{up: true}
	e := newReportingExecutor(link)

	e.reportFinished("tsk_1", finished("tsk_1", 0, "exited", time.Now()))
	require.Equal(t, 1, link.count())

	e.ReplayPending()
	require.Equal(t, 1, link.count(), "replaying after a reconnect must not repeat a report the coordinator already has")
}

func TestAFinishedReportThatCannotGoOutYetStaysForTheNextRegistration(t *testing.T) {
	link := &linkSender{}
	e := newReportingExecutor(link)

	e.reportFinished("tsk_1", finished("tsk_1", 0, "exited", time.Now()))
	e.ReplayPending() // registered, but the connection took nothing (its buffer was full)
	require.Equal(t, 0, link.count())

	link.setUp(true)
	e.ReplayPending()
	require.Equal(t, 1, link.count(), "it was kept, not lost, when the first replay could not send it")
}

func TestASenderThatCannotSayWhetherItDeliveredIsTakenToHave(t *testing.T) {
	plain := &plainSender{}
	e := newReportingExecutor(plain)

	e.reportFinished("tsk_1", finished("tsk_1", 0, "exited", time.Now()))
	e.ReplayPending()
	plain.mu.Lock()
	defer plain.mu.Unlock()
	require.Len(t, plain.sent, 1, "sent once, and not kept for a replay")
}
