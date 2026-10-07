// Package conn owns the agent's one long-lived connection to the
// coordinator: register, heartbeat, and automatic reconnect with backoff.
// It depends only on the generated lazycakev1.AgentServiceClient interface,
// never on a concrete gRPC connection, so tests can inject a fake client.
package conn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/mkassab215/lazycake/internal/clock"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// Identity is the static information the agent presents at registration.
type Identity struct {
	Token      string
	Hostname   string
	Arch       string
	CPUFlags   []string
	Caps       *lazycakev1.Capabilities
	Offer      *lazycakev1.Offer
	BootID     string
	InstanceID string
	Images     []*lazycakev1.CachedImage
}

// Handlers are called as messages arrive from the coordinator. Any of them
// may be nil, in which case the message is ignored.
type Handlers struct {
	OnRegistered func(ack *lazycakev1.RegisterAck)
	OnDispatch   func(ctx context.Context, d *lazycakev1.Dispatch)
	OnCancel     func(ctx context.Context, c *lazycakev1.Cancel)
	// OnShutdown, if set, is called on receipt of a Shutdown message - the
	// caller's job is to trigger the same graceful-shutdown path an OS
	// SIGTERM would (see cmd/agent/run.go), not to do anything special
	// here; Runner just delivers the message.
	OnShutdown     func(ctx context.Context, s *lazycakev1.Shutdown)
	RunningTaskIDs func() []string // polled for every heartbeat
	// Usage, if set, is read for every heartbeat (task 8.15). It may return nil.
	// Display only: it is never used for billing or trust.
	Usage func(ctx context.Context) *lazycakev1.UsageSample
}

// Runner drives one Connect stream at a time and reconnects with backoff
// when it drops, until its context is cancelled.
type Runner struct {
	Client   lazycakev1.AgentServiceClient
	Identity Identity
	Handlers Handlers
	Clock    clock.Clock
	Log      *slog.Logger
	// Backoff controls reconnect timing; defaults to NewBackoff() (1s..60s)
	// if nil. Exposed so tests can inject a fast backoff instead of
	// sleeping through real reconnect delays.
	Backoff *Backoff

	// SendFn lets callers push AgentMessage values onto the active stream
	// (used by the agent's dispatch-result reporting, task 1.8+). It is set
	// internally once a stream is live; Send is a no-op before that.
	sendCh chan *lazycakev1.AgentMessage

	// Fence deadline tracking (PLAN.md "Lease and fencing", task 3.1's
	// invariant): measured from when a heartbeat was *sent*, extended only
	// once that heartbeat's ack arrives - see recordHeartbeatSent/
	// extendFenceDeadline. Guarded by mu since sendLoop and recvLoop (and
	// whatever's watching FenceDeadline) run concurrently.
	mu              sync.Mutex
	leaseS          int32
	fenceDeadline   time.Time
	heartbeatSentAt map[int64]time.Time
}

// FenceDeadline returns the current self-fence deadline: the agent must
// kill every running container if its own clock passes this point without
// having extended it via a fresh acked heartbeat. Zero until the first
// RegisterAck.
func (r *Runner) FenceDeadline() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fenceDeadline
}

func (r *Runner) recordHeartbeatSent(seq int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.heartbeatSentAt[seq] = r.clockNow()
}

// extendFenceDeadline is called when heartbeat seq's ack arrives: the
// fence deadline moves to that heartbeat's *send* time plus the lease,
// never backward, and every earlier pending send-time entry is dropped -
// acks arrive in the order they were sent over a single ordered stream,
// so anything older than seq will never be acked on its own and would
// otherwise leak forever.
func (r *Runner) extendFenceDeadline(seq int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sentAt, ok := r.heartbeatSentAt[seq]
	if !ok {
		return
	}
	for s := range r.heartbeatSentAt {
		if s <= seq {
			delete(r.heartbeatSentAt, s)
		}
	}
	candidate := sentAt.Add(time.Duration(r.leaseS) * time.Second)
	if candidate.After(r.fenceDeadline) {
		r.fenceDeadline = candidate
	}
}

func (r *Runner) clockNow() time.Time {
	if r.Clock == nil {
		return time.Now()
	}
	return r.Clock.Now()
}

// Send queues an outbound message for the active stream. It drops the
// message if no stream is currently up, since there is nothing useful to
// retry onto - the caller's own state (task status in the store-of-record)
// is what reconnection re-announces.
func (r *Runner) Send(msg *lazycakev1.AgentMessage) {
	r.mu.Lock()
	ch := r.sendCh
	r.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
		r.Log.Warn("agent send buffer full, dropping message")
	}
}

// Run connects, registers, heartbeats, and reconnects on failure until ctx
// is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	backoff := r.Backoff
	if backoff == nil {
		backoff = NewBackoff()
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := r.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			r.Log.Warn("connection to coordinator lost", "error", err)
		}
		d := backoff.Next()
		r.Log.Info("reconnecting to coordinator", "in", d)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) error {
	stream, err := r.Client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("opening connect stream: %w", err)
	}

	var running []string
	if r.Handlers.RunningTaskIDs != nil {
		running = r.Handlers.RunningTaskIDs()
	}
	if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Register{
		Register: &lazycakev1.Register{
			Token:          r.Identity.Token,
			Hostname:       r.Identity.Hostname,
			Arch:           r.Identity.Arch,
			CpuFlags:       r.Identity.CPUFlags,
			Caps:           r.Identity.Caps,
			Offer:          r.Identity.Offer,
			BootId:         r.Identity.BootID,
			InstanceId:     r.Identity.InstanceID,
			Images:         r.Identity.Images,
			RunningTaskIds: running,
		},
	}}); err != nil {
		return fmt.Errorf("sending register: %w", err)
	}

	first, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("waiting for register ack: %w", err)
	}
	ack := first.GetRegisterAck()
	if ack == nil {
		return fmt.Errorf("expected RegisterAck, got %T", first.GetBody())
	}
	r.Log.Info("registered with coordinator", "node_id", ack.GetNodeId(), "heartbeat_s", ack.GetHeartbeatS())
	r.mu.Lock()
	r.leaseS = ack.GetLeaseS()
	r.heartbeatSentAt = make(map[int64]time.Time)
	// A fresh successful register is itself proof of contact - give a
	// full lease from now rather than leaving the deadline at its
	// previous (possibly already-expired) value while waiting for the
	// first heartbeat round trip.
	r.fenceDeadline = r.clockNow().Add(time.Duration(r.leaseS) * time.Second)
	r.mu.Unlock()
	if r.Handlers.OnRegistered != nil {
		r.Handlers.OnRegistered(ack)
	}

	// 64 slots at up to a 64KB log batch each bounds outstanding send
	// backlog to roughly the 4MB task 1.9 asks for when the coordinator is
	// slow or unreachable; Send drops rather than blocking once this fills.
	sendCh := make(chan *lazycakev1.AgentMessage, 64)
	r.mu.Lock()
	r.sendCh = sendCh
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.sendCh = nil
		r.mu.Unlock()
	}()

	heartbeatEvery := time.Duration(ack.GetHeartbeatS()) * time.Second
	if heartbeatEvery <= 0 {
		heartbeatEvery = 15 * time.Second
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)
	go r.sendLoop(streamCtx, stream, sendCh, heartbeatEvery, errCh)
	// recvLoop exits when streamCtx ends (this stream's lifetime) but
	// dispatches handlers with the outer ctx: a task must keep running
	// across a reconnect, not die because this particular stream dropped.
	go r.recvLoop(streamCtx, ctx, stream, errCh)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) sendLoop(ctx context.Context, stream lazycakev1.AgentService_ConnectClient, out <-chan *lazycakev1.AgentMessage, heartbeatEvery time.Duration, errCh chan<- error) {
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()
	var seq int64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			seq++
			var running []string
			if r.Handlers.RunningTaskIDs != nil {
				running = r.Handlers.RunningTaskIDs()
			}
			var usage *lazycakev1.UsageSample
			if r.Handlers.Usage != nil {
				// Bounded: reading a container's stats must never hold up the heartbeat
				// that keeps the fence deadline alive.
				uctx, cancel := context.WithTimeout(ctx, heartbeatEvery/3)
				usage = r.Handlers.Usage(uctx)
				cancel()
			}
			r.recordHeartbeatSent(seq)
			if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Heartbeat{
				Heartbeat: &lazycakev1.Heartbeat{Seq: seq, RunningTaskIds: running, Usage: usage},
			}}); err != nil {
				select {
				case errCh <- fmt.Errorf("sending heartbeat: %w", err):
				default:
				}
				return
			}
		case msg, ok := <-out:
			if !ok {
				return
			}
			if err := stream.Send(msg); err != nil {
				select {
				case errCh <- fmt.Errorf("sending message: %w", err):
				default:
				}
				return
			}
		}
	}
}

func (r *Runner) recvLoop(streamCtx, taskCtx context.Context, stream lazycakev1.AgentService_ConnectClient, errCh chan<- error) {
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			select {
			case errCh <- nil:
			default:
			}
			return
		}
		if err != nil {
			select {
			case errCh <- fmt.Errorf("receiving message: %w", err):
			default:
			}
			return
		}
		switch body := msg.GetBody().(type) {
		case *lazycakev1.CoordinatorMessage_Dispatch:
			if r.Handlers.OnDispatch != nil {
				r.Handlers.OnDispatch(taskCtx, body.Dispatch)
			}
		case *lazycakev1.CoordinatorMessage_Cancel:
			if r.Handlers.OnCancel != nil {
				r.Handlers.OnCancel(taskCtx, body.Cancel)
			}
		case *lazycakev1.CoordinatorMessage_Shutdown:
			if r.Handlers.OnShutdown != nil {
				r.Handlers.OnShutdown(taskCtx, body.Shutdown)
			}
		case *lazycakev1.CoordinatorMessage_HeartbeatAck:
			r.extendFenceDeadline(body.HeartbeatAck.GetSeq())
		case *lazycakev1.CoordinatorMessage_RegisterAck:
			// no-op: already handled where Register was sent
		}
		if streamCtx.Err() != nil {
			return
		}
	}
}
