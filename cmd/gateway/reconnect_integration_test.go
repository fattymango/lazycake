//go:build integration

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/mkassab215/lazycake/internal/gateway/config"
	"github.com/mkassab215/lazycake/internal/gateway/listener"
	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

type okAuth struct{}

func (okAuth) Authenticate(context.Context, string) (string, error) { return "act_test", nil }

type countingRegistry struct {
	mu       sync.Mutex
	connects int
}

func (r *countingRegistry) OwnsGateway(context.Context, string, string) (bool, error) {
	return true, nil
}
func (r *countingRegistry) SetGatewayConnected(_ context.Context, _ string, connected bool, _ []byte) error {
	if connected {
		r.mu.Lock()
		r.connects++
		r.mu.Unlock()
	}
	return nil
}
func (r *countingRegistry) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.connects }

// The real failure this guards: the coordinator restarts and the gateway
// must come back by itself, with no process supervisor to restart it.
func TestGatewayReconnectsAfterRelayRestart(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := &countingRegistry{}
	startRelay := func() context.CancelFunc {
		ctx, cancel := context.WithCancel(context.Background())
		r := &quic.Relay{Auth: okAuth{}, Gateways: reg, Log: log}
		go r.Serve(ctx, addr)
		time.Sleep(150 * time.Millisecond)
		return cancel
	}

	shrinkBackoff(t)
	reconnectMin = 100 * time.Millisecond

	kp, err := noise.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{CoordinatorAddr: addr, Token: "t", GatewayID: "gw_1"}

	stopRelay := startRelay()
	gwCtx, stopGateway := context.WithCancel(context.Background())
	defer stopGateway()
	gwDone := make(chan struct{})
	go func() {
		defer close(gwDone)
		serveForever(gwCtx, log, func(ctx context.Context) error {
			return serveOnce(ctx, cfg, kp, map[string]string{"files": "127.0.0.1:1"}, log, func(listener.ForwardReport) {})
		})
	}()

	waitFor := func(n int, what string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for reg.count() < n {
			if time.Now().After(deadline) {
				t.Fatalf("%s: gateway connected %d times, want %d", what, reg.count(), n)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	waitFor(1, "initial connect")

	stopRelay() // relay shutting down closes the gateway's connection
	time.Sleep(300 * time.Millisecond)
	stopRelay = startRelay()
	defer stopRelay()
	waitFor(2, fmt.Sprintf("reconnect after relay restart (gateway still running: %v)", gwCtx.Err() == nil))

	stopGateway()
	select {
	case <-gwDone:
	case <-time.After(5 * time.Second):
		t.Fatal("gateway didn't stop on cancel")
	}
}
