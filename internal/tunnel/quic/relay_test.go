//go:build integration

package quic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

type fakeAuth struct{}

func (fakeAuth) Authenticate(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("empty token")
	}
	return "act_test", nil
}

func freeUDPAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()
	return addr
}

// TestRelayRoundTrip proves task 2.3's contract: an agent opens a stream
// tagged for a gateway/task, the gateway receives it, bytes flow both
// ways, and the relay's own byte counters match what was actually sent.
func TestRelayRoundTrip(t *testing.T) {
	addr := freeUDPAddr(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	statsCh := make(chan StreamStats, 1)
	relay := &Relay{Auth: fakeAuth{}, Log: log, OnStreamClosed: func(s StreamStats) { statsCh <- s }}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- relay.Serve(ctx, addr) }()
	time.Sleep(100 * time.Millisecond) // let the listener come up

	gwConn, err := DialGateway(ctx, addr, "gw-token", "gw_1", nil)
	if err != nil {
		t.Fatalf("DialGateway: %v", err)
	}

	agentConn, err := DialAgent(ctx, addr, "agent-token")
	if err != nil {
		t.Fatalf("DialAgent: %v", err)
	}

	agentPayload := bytes.Repeat([]byte("A"), 50000)
	gwPayload := bytes.Repeat([]byte("B"), 70000)

	gwDone := make(chan error, 1)
	go func() {
		stream, taskID, err := AcceptRelayedStream(ctx, gwConn)
		if err != nil {
			gwDone <- fmt.Errorf("AcceptRelayedStream: %w", err)
			return
		}
		if taskID != "tsk_1" {
			gwDone <- fmt.Errorf("expected task id tsk_1, got %s", taskID)
			return
		}
		got := make([]byte, len(agentPayload))
		if _, err := io.ReadFull(stream, got); err != nil {
			gwDone <- fmt.Errorf("reading agent payload: %w", err)
			return
		}
		if !bytes.Equal(got, agentPayload) {
			gwDone <- fmt.Errorf("agent payload mismatch")
			return
		}
		if _, err := stream.Write(gwPayload); err != nil {
			gwDone <- fmt.Errorf("writing gateway payload: %w", err)
			return
		}
		stream.Close()
		gwDone <- nil
	}()

	agentStream, err := OpenRelayedStream(ctx, agentConn, "gw_1", "tsk_1")
	if err != nil {
		t.Fatalf("OpenRelayedStream: %v", err)
	}
	if _, err := agentStream.Write(agentPayload); err != nil {
		t.Fatalf("writing agent payload: %v", err)
	}
	got := make([]byte, len(gwPayload))
	if _, err := io.ReadFull(agentStream, got); err != nil {
		t.Fatalf("reading gateway payload: %v", err)
	}
	if !bytes.Equal(got, gwPayload) {
		t.Fatal("gateway payload mismatch")
	}
	agentStream.Close()

	if err := <-gwDone; err != nil {
		t.Fatal(err)
	}

	select {
	case stats := <-statsCh:
		if stats.BytesAgentToGW != int64(len(agentPayload)) {
			t.Fatalf("relay counted %d agent->gw bytes, want %d", stats.BytesAgentToGW, len(agentPayload))
		}
		if stats.BytesGWToAgent != int64(len(gwPayload)) {
			t.Fatalf("relay counted %d gw->agent bytes, want %d", stats.BytesGWToAgent, len(gwPayload))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for relay stream-closed stats")
	}
}

type recordingRegistry struct {
	mu    sync.Mutex
	calls []bool // the connected value of every SetGatewayConnected call, in order
}

func (r *recordingRegistry) OwnsGateway(context.Context, string, string) (bool, error) {
	return true, nil
}
func (r *recordingRegistry) SetGatewayConnected(_ context.Context, _ string, connected bool, _ []byte) error {
	r.mu.Lock()
	r.calls = append(r.calls, connected)
	r.mu.Unlock()
	return nil
}
func (r *recordingRegistry) last() (bool, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return false, 0
	}
	return r.calls[len(r.calls)-1], len(r.calls)
}

// A gateway that reconnects before its old connection has timed out must
// stay marked connected: the old connection's late cleanup used to flip it
// to disconnected, leaving a live gateway showing as down.
func TestGatewayReconnectStaysConnected(t *testing.T) {
	addr := freeUDPAddr(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	reg := &recordingRegistry{}
	relay := &Relay{Auth: fakeAuth{}, Gateways: reg, Log: log}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go relay.Serve(ctx, addr)
	time.Sleep(100 * time.Millisecond)

	old, err := DialGateway(ctx, addr, "tok", "gw_1", nil)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	fresh, err := DialGateway(ctx, addr, "tok", "gw_1", nil) // same gateway ID, newer connection
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.CloseWithError(0, "")
	time.Sleep(300 * time.Millisecond) // the relay closes the superseded connection

	select {
	case <-old.Context().Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the superseded connection was never closed")
	}
	time.Sleep(200 * time.Millisecond) // let its cleanup run
	if last, n := reg.last(); !last {
		t.Fatalf("after a reconnect the gateway was last recorded disconnected (%d calls)", n)
	}

	fresh.CloseWithError(0, "bye") // a real disconnect must still be recorded
	deadline := time.Now().Add(3 * time.Second)
	for {
		if last, _ := reg.last(); !last {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a genuine disconnect was never recorded")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Shutting the relay down must tell connected peers straight away rather
// than leaving them to discover it at the idle timeout.
func TestRelayShutdownClosesConnections(t *testing.T) {
	addr := freeUDPAddr(t)
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	relay := &Relay{Auth: fakeAuth{}, Log: log}

	ctx, cancel := context.WithCancel(context.Background())
	go relay.Serve(ctx, addr)
	time.Sleep(100 * time.Millisecond)

	gw, err := DialGateway(context.Background(), addr, "tok", "gw_1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.CloseWithError(0, "")
	time.Sleep(100 * time.Millisecond)

	cancel()
	select {
	case <-gw.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("gateway connection still open 2s after the relay shut down")
	}
}
