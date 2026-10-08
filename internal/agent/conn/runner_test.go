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

// A heartbeat carries the usage reading (task 8.15), and a Usage that blocks
// forever can't hold the heartbeat up: it runs under a short deadline.
func TestHeartbeatCarriesUsageAndAStuckReaderCannotStallIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		usage   func(ctx context.Context) *lazycakev1.UsageSample
		wantMem int64
	}{
		{"carries the reading", func(context.Context) *lazycakev1.UsageSample {
			return &lazycakev1.UsageSample{HostMemUsedBytes: 42}
		}, 42},
		{"a reader that never returns on its own", func(ctx context.Context) *lazycakev1.UsageSample {
			<-ctx.Done()
			return nil
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			streams := make(chan *fakeStream, 4)
			client := &fakeClient{next: func(ctx context.Context) *fakeStream {
				s := newFakeStream(ctx)
				streams <- s
				return s
			}}
			r := &Runner{
				Client:   client,
				Identity: Identity{Token: "tok", Hostname: "h1", Arch: "amd64"},
				Handlers: Handlers{Usage: tc.usage},
				Log:      discardLogger(),
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go r.Run(ctx)

			s := waitFor(t, streams, time.Second, "first connect")
			waitFor(t, s.out, time.Second, "register message")
			s.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_RegisterAck{
				RegisterAck: &lazycakev1.RegisterAck{NodeId: "nod_1", HeartbeatS: 1, LeaseS: 5},
			}}
			hb := waitFor(t, s.out, 3*time.Second, "heartbeat message")
			if hb.GetHeartbeat() == nil {
				t.Fatalf("expected Heartbeat, got %+v", hb)
			}
			if got := hb.GetHeartbeat().GetUsage().GetHostMemUsedBytes(); got != tc.wantMem {
				t.Fatalf("usage mem = %d, want %d", got, tc.wantMem)
			}
		})
	}
}

// A connection that dies silently (a suspended laptop waking up) gives no error: sending still succeeds and nothing
// ever answers. The agent must notice the silence and open a new stream, not wait forever.
func TestRunnerReconnectsWhenTheCoordinatorStopsAnswering(t *testing.T) {
	streams := make(chan *fakeStream, 8)
	client := &fakeClient{next: func(ctx context.Context) *fakeStream {
		s := newFakeStream(ctx)
		streams <- s
		return s
	}}
	r := &Runner{
		Client:     client,
		Identity:   Identity{Token: "tok", Hostname: "h1", Arch: "amd64"},
		Log:        discardLogger(),
		AckTimeout: 1500 * time.Millisecond,
		Backoff:    &Backoff{Min: 10 * time.Millisecond, Max: 20 * time.Millisecond},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	first := waitFor(t, streams, time.Second, "first connect")
	waitFor(t, first.out, time.Second, "register")
	first.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_RegisterAck{
		RegisterAck: &lazycakev1.RegisterAck{NodeId: "nod_1", HeartbeatS: 1, LeaseS: 5},
	}}
	// Heartbeats are sent (and nobody answers any of them).
	waitFor(t, first.out, 3*time.Second, "a heartbeat")

	// Within a few seconds the silent stream is abandoned and a new one is opened.
	second := waitFor(t, streams, 6*time.Second, "a reconnect after the coordinator went silent")
	if second == first {
		t.Fatal("expected a fresh stream")
	}
}

// A connection that is answering is left alone.
func TestRunnerKeepsAStreamThatIsAnswering(t *testing.T) {
	streams := make(chan *fakeStream, 8)
	client := &fakeClient{next: func(ctx context.Context) *fakeStream {
		s := newFakeStream(ctx)
		streams <- s
		return s
	}}
	r := &Runner{
		Client:     client,
		Identity:   Identity{Token: "tok", Hostname: "h1", Arch: "amd64"},
		Log:        discardLogger(),
		AckTimeout: 1500 * time.Millisecond,
		Backoff:    &Backoff{Min: 10 * time.Millisecond, Max: 20 * time.Millisecond},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	first := waitFor(t, streams, time.Second, "first connect")
	waitFor(t, first.out, time.Second, "register")
	first.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_RegisterAck{
		RegisterAck: &lazycakev1.RegisterAck{NodeId: "nod_1", HeartbeatS: 1, LeaseS: 5},
	}}
	// Answer every heartbeat for longer than the timeout.
	deadline := time.After(4 * time.Second)
	for {
		select {
		case msg := <-first.out:
			if hb := msg.GetHeartbeat(); hb != nil {
				first.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_HeartbeatAck{HeartbeatAck: &lazycakev1.HeartbeatAck{Seq: hb.GetSeq()}}}
			}
		case <-deadline:
			select {
			case <-streams:
				t.Fatal("a stream that answers every heartbeat must not be abandoned")
			default:
			}
			return
		}
	}
}
