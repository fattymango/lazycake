//go:build integration

package listener

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

// A connection that stays open must show up while it is open, not only when it
// closes: it reports deltas every progressEvery, and those deltas (plus the final
// one) sum to exactly what moved. Without this a two-hour database session would
// be invisible until it ended.
func TestLongLivedConnectionReportsProgressThatSumsToTheTotal(t *testing.T) {
	old := progressEvery
	progressEvery = 150 * time.Millisecond
	t.Cleanup(func() { progressEvery = old })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	addr := freeUDPAddr(t)
	relay := &quic.Relay{Auth: fakeAuth{}, Log: log}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go relay.Serve(ctx, addr)
	time.Sleep(100 * time.Millisecond)

	svcPort := startFakeService(t)
	gwKeypair, err := noise.GenerateKeypair()
	require.NoError(t, err)
	gwConn, err := quic.DialGateway(ctx, addr, "gw-token", "gw_1", gwKeypair.Public)
	require.NoError(t, err)

	var mu sync.Mutex
	var reports []ForwardReport
	totals := make(chan ForwardStats, 1)
	l := &Listener{
		Conn: gwConn, Keypair: gwKeypair, Log: log,
		Services:  map[string]string{"db": fmt.Sprintf("127.0.0.1:%d", svcPort)},
		OnReport:  func(r ForwardReport) { mu.Lock(); reports = append(reports, r); mu.Unlock() },
		OnForward: func(s ForwardStats) { totals <- s },
	}
	go l.Run(ctx)

	agentConn, err := quic.DialAgent(ctx, addr, "agent-token")
	require.NoError(t, err)
	stream, err := quic.OpenRelayedStream(ctx, agentConn, "gw_1", "tsk_long")
	require.NoError(t, err)
	session, err := noise.DoInitiatorHandshake(stream, mustKeypair(t), gwKeypair.Public)
	require.NoError(t, err)
	_, err = session.Write([]byte("db"))
	require.NoError(t, err)

	// Keep the connection open for ~1s, sending something every 100ms; the echo
	// service returns every byte, so both directions move.
	chunk := bytes.Repeat([]byte("x"), 1000)
	sent := 0
	for i := 0; i < 10; i++ {
		_, err := session.Write(chunk)
		require.NoError(t, err)
		_, err = io.ReadFull(session, make([]byte, len(chunk)))
		require.NoError(t, err)
		sent += len(chunk)
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	whileOpen := len(reports)
	mu.Unlock()
	require.GreaterOrEqual(t, whileOpen, 2, "an open connection reports while it is still open")
	stream.Close()

	var total ForwardStats
	select {
	case total = <-totals:
	case <-time.After(5 * time.Second):
		t.Fatal("the connection never closed")
	}

	mu.Lock()
	defer mu.Unlock()
	var sumLocal, sumTask int64
	finals := 0
	for i, r := range reports {
		require.Equal(t, "tsk_long", r.TaskID)
		require.Equal(t, "db", r.Service)
		require.GreaterOrEqual(t, r.ToLocal, int64(0))
		require.GreaterOrEqual(t, r.ToTask, int64(0))
		sumLocal += r.ToLocal
		sumTask += r.ToTask
		if r.Final {
			finals++
			require.Equal(t, len(reports)-1, i, "the final report is the last one")
		}
	}
	require.Equal(t, 1, finals, "exactly one final report, so the connection counts once")
	require.Equal(t, total.BytesToLocal, sumLocal, "deltas sum to the connection's total (to the service)")
	require.Equal(t, total.BytesToTask, sumTask, "deltas sum to the connection's total (back to the task)")
	require.GreaterOrEqual(t, sumLocal, int64(sent), "at least the application bytes moved (the tunnel adds framing)")
}
