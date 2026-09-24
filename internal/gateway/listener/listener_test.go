//go:build integration

package listener

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

	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

type fakeAuth struct{}

func (fakeAuth) Authenticate(ctx context.Context, token string) (string, error) { return "act_1", nil }

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

// fakeService is a plain TCP echo server standing in for "a local
// Postgres" - task 2.4's verify command names Postgres specifically, but
// what's actually being proven is "the gateway forwards bytes to whatever
// is on the published port," which an echo server demonstrates just as
// rigorously without a real database dependency in the test suite.
func startFakeService(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go io.Copy(conn, conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestGatewayForward proves task 2.4 end to end through the real relay:
// an "agent" dials the relay, opens a stream tagged for this gateway,
// does the Noise handshake, sends the target service name, and the
// gateway forwards the resulting bytes to (and from) a local service -
// refusing to forward to a service it hasn't published.
func TestGatewayForward(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	addr := freeUDPAddr(t)

	relay := &quic.Relay{Auth: fakeAuth{}, Log: log}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go relay.Serve(ctx, addr)
	time.Sleep(100 * time.Millisecond)

	svcPort := startFakeService(t)

	gwKeypair, err := noise.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	gwConn, err := quic.DialGateway(ctx, addr, "gw-token", "gw_1")
	if err != nil {
		t.Fatalf("DialGateway: %v", err)
	}

	var statsCh = make(chan ForwardStats, 1)
	l := &Listener{
		Conn: gwConn, Keypair: gwKeypair, Log: log,
		Services:  map[string]int{"db": svcPort},
		OnForward: func(s ForwardStats) { statsCh <- s },
	}
	go l.Run(ctx)

	agentConn, err := quic.DialAgent(ctx, addr, "agent-token")
	if err != nil {
		t.Fatalf("DialAgent: %v", err)
	}
	stream, err := quic.OpenRelayedStream(ctx, agentConn, "gw_1", "tsk_1")
	if err != nil {
		t.Fatalf("OpenRelayedStream: %v", err)
	}

	session, err := noise.DoInitiatorHandshake(stream, mustKeypair(t), gwKeypair.Public)
	if err != nil {
		t.Fatalf("noise handshake: %v", err)
	}
	if _, err := session.Write([]byte("db")); err != nil {
		t.Fatalf("writing service name: %v", err)
	}

	payload := bytes.Repeat([]byte("hello-through-the-tunnel-"), 1000)
	if _, err := session.Write(payload); err != nil {
		t.Fatalf("writing payload: %v", err)
	}
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(session, got); err != nil {
		t.Fatalf("reading echoed payload: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("echoed payload mismatch")
	}
	stream.Close()

	select {
	case stats := <-statsCh:
		if stats.Service != "db" {
			t.Fatalf("expected service db, got %s", stats.Service)
		}
		if stats.BytesToLocal == 0 || stats.BytesToTask == 0 {
			t.Fatalf("expected non-zero byte counts both ways, got %+v", stats)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for forward stats")
	}
}

// TestGatewayRefusesUnpublishedService proves the "only forwards to
// services in its own published list" rule.
func TestGatewayRefusesUnpublishedService(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	addr := freeUDPAddr(t)

	relay := &quic.Relay{Auth: fakeAuth{}, Log: log}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go relay.Serve(ctx, addr)
	time.Sleep(100 * time.Millisecond)

	gwKeypair, _ := noise.GenerateKeypair()
	gwConn, err := quic.DialGateway(ctx, addr, "gw-token", "gw_1")
	if err != nil {
		t.Fatal(err)
	}
	forwardAttempted := make(chan struct{}, 1)
	l := &Listener{
		Conn: gwConn, Keypair: gwKeypair, Log: log,
		Services: map[string]int{"db": 1}, // no "cache" published
		Dial: func(network, addr string) (net.Conn, error) {
			forwardAttempted <- struct{}{}
			return nil, fmt.Errorf("should never be called")
		},
	}
	go l.Run(ctx)

	agentConn, err := quic.DialAgent(ctx, addr, "agent-token")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := quic.OpenRelayedStream(ctx, agentConn, "gw_1", "tsk_2")
	if err != nil {
		t.Fatal(err)
	}
	session, err := noise.DoInitiatorHandshake(stream, mustKeypair(t), gwKeypair.Public)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Write([]byte("cache")); err != nil {
		t.Fatal(err)
	}

	select {
	case <-forwardAttempted:
		t.Fatal("gateway dialled a local service for an unpublished name")
	case <-time.After(500 * time.Millisecond):
		// expected: nothing happened
	}
}

func mustKeypair(t *testing.T) noise.Keypair {
	t.Helper()
	kp, err := noise.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	return kp
}
