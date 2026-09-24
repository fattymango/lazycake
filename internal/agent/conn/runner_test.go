package conn

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitFor[T any](t *testing.T, ch chan T, d time.Duration, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(d):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func TestRunnerRegistersAndHeartbeats(t *testing.T) {
	streams := make(chan *fakeStream, 4)
	client := &fakeClient{next: func(ctx context.Context) *fakeStream {
		s := newFakeStream(ctx)
		streams <- s
		return s
	}}

	r := &Runner{
		Client:   client,
		Identity: Identity{Token: "tok", Hostname: "h1", Arch: "amd64"},
		Log:      discardLogger(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	s := waitFor(t, streams, time.Second, "first connect")

	reg := waitFor(t, s.out, time.Second, "register message")
	if reg.GetRegister() == nil || reg.GetRegister().GetToken() != "tok" {
		t.Fatalf("expected Register with token, got %+v", reg)
	}

	s.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_RegisterAck{
		RegisterAck: &lazycakev1.RegisterAck{NodeId: "nod_1", HeartbeatS: 1, LeaseS: 5},
	}}

	hb := waitFor(t, s.out, 3*time.Second, "heartbeat message")
	if hb.GetHeartbeat() == nil {
		t.Fatalf("expected Heartbeat, got %+v", hb)
	}
}

func TestRunnerReconnectsOnStreamError(t *testing.T) {
	streams := make(chan *fakeStream, 4)
	client := &fakeClient{next: func(ctx context.Context) *fakeStream {
		s := newFakeStream(ctx)
		streams <- s
		return s
	}}

	fastBackoff := &Backoff{Min: time.Millisecond, Max: 10 * time.Millisecond}

	r := &Runner{
		Client:   client,
		Identity: Identity{Token: "tok"},
		Log:      discardLogger(),
		Backoff:  fastBackoff,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	s1 := waitFor(t, streams, time.Second, "first connect")
	waitFor(t, s1.out, time.Second, "first register")
	s1.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_RegisterAck{
		RegisterAck: &lazycakev1.RegisterAck{NodeId: "nod_1", HeartbeatS: 30},
	}}

	s1.failRecv(errors.New("simulated coordinator restart"))

	s2 := waitFor(t, streams, 2*time.Second, "reconnect")
	reg2 := waitFor(t, s2.out, time.Second, "second register")
	if reg2.GetRegister() == nil {
		t.Fatalf("expected Register on reconnect, got %+v", reg2)
	}
	if client.connectCount() < 2 {
		t.Fatalf("expected at least 2 Connect calls, got %d", client.connectCount())
	}
}
