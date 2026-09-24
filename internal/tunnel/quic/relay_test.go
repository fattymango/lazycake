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
