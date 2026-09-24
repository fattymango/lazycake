//go:build integration

// Task 3.2's own verify: a real podman container, dispatched through the
// real conn.Runner + exec.Executor + lease.Watcher wiring exactly as
// cmd/agent assembles them, must die on its own within lease_s+10s once the
// agent can no longer hear the coordinator - simulating what an iptables
// DROP of the agent's outbound connection would do, without needing actual
// firewall access in this sandbox (see task 3.2's "Done when" in
// IMPLEMENTATION.md for the iptables framing this stands in for).
package lease

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/conn"
	lcexec "github.com/mkassab215/lazycake/internal/agent/exec"
	lcruntime "github.com/mkassab215/lazycake/internal/agent/runtime"
	"github.com/mkassab215/lazycake/internal/id"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

const probeImage = "docker.io/library/alpine:latest"

// blockableStream is a hand-rolled double for the generated bidi-streaming
// client: Send always succeeds (so the agent's own TaskStarted/TaskFinished
// reports are still observable in tests), but once blocked() is set, Recv
// never delivers another CoordinatorMessage - modelling an iptables DROP of
// inbound traffic on the agent's side of the connection, which is exactly
// what starves HeartbeatAcks and lets the fence deadline actually pass.
type blockableStream struct {
	ctx        context.Context
	in         chan *lazycakev1.CoordinatorMessage
	out        chan *lazycakev1.AgentMessage
	blocked    atomic.Bool
	heartbeatS int32
	leaseS     int32
}

func newBlockableStream(ctx context.Context, heartbeatS, leaseS int32) *blockableStream {
	return &blockableStream{
		ctx: ctx, in: make(chan *lazycakev1.CoordinatorMessage, 32), out: make(chan *lazycakev1.AgentMessage, 256),
		heartbeatS: heartbeatS, leaseS: leaseS,
	}
}

func (s *blockableStream) block() { s.blocked.Store(true) }

// Send both hands the message to the test's own consumer (via out, for
// waitForBody to observe) and, inline, plays the part of "the coordinator"
// for the two message types that need a reply - Register and Heartbeat -
// exactly like autoAck used to as a separate goroutine, except a single
// consumer of out avoids racing waitForBody for the same messages.
func (s *blockableStream) Send(m *lazycakev1.AgentMessage) error {
	switch body := m.GetBody().(type) {
	case *lazycakev1.AgentMessage_Register:
		s.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_RegisterAck{
			RegisterAck: &lazycakev1.RegisterAck{NodeId: "node_test", HeartbeatS: s.heartbeatS, LeaseS: s.leaseS},
		}}
	case *lazycakev1.AgentMessage_Heartbeat:
		if !s.blocked.Load() {
			s.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_HeartbeatAck{
				HeartbeatAck: &lazycakev1.HeartbeatAck{Seq: body.Heartbeat.GetSeq()},
			}}
		}
	}
	select {
	case s.out <- m:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *blockableStream) Recv() (*lazycakev1.CoordinatorMessage, error) {
	if s.blocked.Load() {
		<-s.ctx.Done()
		return nil, s.ctx.Err()
	}
	select {
	case m, ok := <-s.in:
		if !ok {
			return nil, io.EOF
		}
		return m, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *blockableStream) Header() (metadata.MD, error) { return nil, nil }
func (s *blockableStream) Trailer() metadata.MD         { return nil }
func (s *blockableStream) CloseSend() error             { return nil }
func (s *blockableStream) Context() context.Context     { return s.ctx }
func (s *blockableStream) SendMsg(m any) error          { return errors.New("unused") }
func (s *blockableStream) RecvMsg(m any) error          { return errors.New("unused") }

var _ grpc.BidiStreamingClient[lazycakev1.AgentMessage, lazycakev1.CoordinatorMessage] = (*blockableStream)(nil)

type blockableClient struct{ stream *blockableStream }

func (c *blockableClient) Connect(ctx context.Context, _ ...grpc.CallOption) (lazycakev1.AgentService_ConnectClient, error) {
	return c.stream, nil
}

var _ lazycakev1.AgentServiceClient = (*blockableClient)(nil)

func TestSelfFence(t *testing.T) {
	sock := os.Getenv("LAZYCAKE_TEST_PODMAN_SOCKET")
	var err error
	if sock == "" {
		sock, err = lcruntime.DefaultSocket()
		require.NoError(t, err)
	}
	rt, err := lcruntime.NewPodmanRuntime(sock)
	require.NoError(t, err)
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	_, err = rt.Pull(ctx, probeImage)
	require.NoError(t, err)

	stream := newBlockableStream(ctx, 1 /* heartbeat_s */, 3 /* lease_s */)

	ledger := capacity.NewLedger(capacity.Resources{Cores: 2, MemoryMB: 2048, DiskMB: 10000})
	runner := &conn.Runner{
		Client: &blockableClient{stream: stream},
		Identity: conn.Identity{
			Token: "unused", Hostname: "selffence-test", Arch: "amd64",
			Offer: &lazycakev1.Offer{Cores: 2, MemoryMb: 2048, DiskMb: 10000},
		},
		Log: discardLog(),
	}
	executor := &lcexec.Executor{
		Runtime: rt, Ledger: ledger, Send: runner, Log: discardLog(),
		InstanceID: "ins_selffence", BootID: "boot_selffence",
	}
	runner.Handlers = conn.Handlers{
		OnDispatch:     executor.HandleDispatch,
		RunningTaskIDs: ledger.TaskIDs,
		OnRegistered:   func(*lazycakev1.RegisterAck) { executor.ReplayPending() },
	}
	watcher := &Watcher{
		Deadline:      runner.FenceDeadline,
		Log:           discardLog(),
		CheckInterval: 200 * time.Millisecond,
		OnFence: func() {
			fenceCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			executor.FenceAll(fenceCtx)
		},
	}

	go runner.Run(ctx)
	go watcher.Run(ctx)

	// Wait for registration (a RegisterAck round trip) before dispatching.
	require.Eventually(t, func() bool { return !runner.FenceDeadline().IsZero() }, 10*time.Second, 50*time.Millisecond, "agent never registered")

	taskID := id.New(id.Task)
	t.Cleanup(func() { _ = rt.Remove(context.Background(), "lazycake-"+taskID) })
	executor.HandleDispatch(ctx, &lazycakev1.Dispatch{
		TaskId:     taskID,
		Image:      probeImage,
		Entrypoint: []string{"sh", "-c"},
		Args:       []string{"sleep 300"},
		Isolation:  "podman",
		Limits:     &lazycakev1.Limits{CpuCores: 1, MemoryMb: 128, DiskMb: 500, WallTimeoutS: 280},
	})

	// Wait for TaskStarted so we know the container is actually running
	// before pulling the plug.
	started := waitForBody(t, stream, 20*time.Second, func(b *lazycakev1.AgentMessage) bool {
		_, ok := b.GetBody().(*lazycakev1.AgentMessage_Started)
		return ok
	})
	require.NotNil(t, started, "task never reported started")

	blockedAt := time.Now()
	stream.block()

	finished := waitForBody(t, stream, 20*time.Second, func(b *lazycakev1.AgentMessage) bool {
		f, ok := b.GetBody().(*lazycakev1.AgentMessage_Finished)
		return ok && f.Finished.GetTaskId() == taskID
	})
	require.NotNil(t, finished, "task was never reported finished after the connection was blocked")
	// lease_s(3) to notice + up to 10s of SIGTERM grace (a bare `sh -c
	// sleep`, as PID 1, ignores SIGTERM by default, so this really does run
	// the full grace period before SIGKILL) + a few seconds of slack for
	// watcher polling and podman's own round trip.
	require.Less(t, time.Since(blockedAt), 16*time.Second, "self-fence took longer than lease_s(3)+10s+slack")

	f := finished.GetBody().(*lazycakev1.AgentMessage_Finished).Finished
	require.Equal(t, "fenced", f.GetExitReason())
}

func waitForBody(t *testing.T, stream *blockableStream, timeout time.Duration, match func(*lazycakev1.AgentMessage) bool) *lazycakev1.AgentMessage {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-stream.out:
			if match(msg) {
				return msg
			}
		case <-deadline:
			return nil
		}
	}
}
