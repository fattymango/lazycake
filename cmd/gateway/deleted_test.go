package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	quicgo "github.com/quic-go/quic-go"

	"github.com/fattymango/lazycake/internal/tunnel/quic"
)

// The relay saying "this gateway was deleted" ends the retry loop: more attempts could only be refused again.
func TestServeForeverStopsWhenGatewayDeleted(t *testing.T) {
	shrinkBackoff(t)
	var calls atomic.Int32
	err := serveForever(context.Background(), quietLog(), func(context.Context) error {
		calls.Add(1)
		return fmt.Errorf("accepting relayed stream: %w", &quicgo.ApplicationError{ErrorCode: quic.CodeGatewayRevoked, Remote: true})
	})
	if !errors.Is(err, errGatewayDeleted) {
		t.Fatalf("got %v, want errGatewayDeleted", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("retried %d times after being deleted", calls.Load())
	}

	// Any other failure keeps retrying (the existing behaviour).
	var other atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	_ = serveForever(ctx, quietLog(), func(context.Context) error {
		if other.Add(1) >= 3 {
			cancel()
		}
		return &quicgo.ApplicationError{ErrorCode: 1, Remote: true}
	})
	if other.Load() < 3 {
		t.Fatal("an ordinary rejection must still be retried")
	}
}

// A restart of the container must not bring a deleted gateway back, but a different gateway sharing the
// volume is unaffected.
func TestDeletedMarkerIsPerGateway(t *testing.T) {
	dir := t.TempDir()
	path := tombstonePath(filepath.Join(dir, "noise.key"))
	if revokedBefore(path, "gw_1") {
		t.Fatal("no marker yet")
	}
	if err := os.WriteFile(path, []byte("gw_1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !revokedBefore(path, "gw_1") {
		t.Fatal("marker not honoured")
	}
	if revokedBefore(path, "gw_2") {
		t.Fatal("marker for gw_1 must not stop gw_2")
	}
}

// A dormant gateway holds still until stopped.
func TestDormantWaitsForStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- dormant(ctx, quietLog(), "gw_1") }()
	select {
	case <-done:
		t.Fatal("returned before being stopped")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("didn't stop")
	}
}
