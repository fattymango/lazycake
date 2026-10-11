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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/fattymango/lazycake/internal/agent/capacity"
	"github.com/fattymango/lazycake/internal/agent/conn"
	lcexec "github.com/fattymango/lazycake/internal/agent/exec"
	lcruntime "github.com/fattymango/lazycake/internal/agent/runtime"
	"github.com/fattymango/lazycake/internal/id"
	lazycakev1 "github.com/fattymango/lazycake/internal/proto/lazycake/v1"
)

const probeImage = "docker.io/library/alpine:latest"

// blockableStream is the "network link" between the agent and a pretend coordinator: it records everything the agent sends (so the
// agent's own TaskStarted/TaskFinished reports are observable in tests) and can be blocked, after which the pretend coordinator
// goes silent: no further acknowledgements reach the agent. That models an iptables DROP of inbound traffic on the agent's side
// of the connection, which starves HeartbeatAcks and lets the fence deadline pass. unblock() models the network coming back.
//
// Each Connect call gets its own stream (a blockableConn), exactly as a real gRPC client opens a new stream per connection: an
// agent that notices the silence and reconnects must get a fresh stream, not the old dead one.
type blockableStream struct {
	ctx        context.Context
	out        chan *lazycakev1.AgentMessage
	blocked    atomic.Bool
	unblocked  chan struct{} // closed by unblock(); recreated by block()
	mu         sync.Mutex
	heartbeatS int32
	leaseS     int32
}

func newBlockableStream(ctx context.Context, heartbeatS, leaseS int32) *blockableStream {
	return &blockableStream{
		ctx: ctx, out: make(chan *lazycakev1.AgentMessage, 256),
		unblocked:  make(chan struct{}),
		heartbeatS: heartbeatS, leaseS: leaseS,
	}
}

func (s *blockableStream) block() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.blocked.Load() {
		s.unblocked = make(chan struct{})
		s.blocked.Store(true)
	}
}

func (s *blockableStream) unblock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked.Load() {
		s.blocked.Store(false)
		close(s.unblocked)
	}
}

func (s *blockableStream) waitUnblocked() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unblocked
}

// blockableConn is one stream over the link.
type blockableConn struct {
	link *blockableStream
	in   chan *lazycakev1.CoordinatorMessage
}

// Send both hands the message to the test's own consumer (via link.out, for waitForBody to observe) and, inline, plays the part of
// "the coordinator" for the two message types that need a reply: Register (always answered once the link is up) and Heartbeat.
func (c *blockableConn) Send(m *lazycakev1.AgentMessage) error {
	switch body := m.GetBody().(type) {
	case *lazycakev1.AgentMessage_Register:
		c.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_RegisterAck{
			RegisterAck: &lazycakev1.RegisterAck{NodeId: "node_test", HeartbeatS: c.link.heartbeatS, LeaseS: c.link.leaseS},
		}}
	case *lazycakev1.AgentMessage_Heartbeat:
		if !c.link.blocked.Load() {
			c.in <- &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_HeartbeatAck{
				HeartbeatAck: &lazycakev1.HeartbeatAck{Seq: body.Heartbeat.GetSeq()},
			}}
		}
	}
	select {
	case c.link.out <- m:
		return nil
	case <-c.link.ctx.Done():
		return c.link.ctx.Err()
	}
}

func (c *blockableConn) Recv() (*lazycakev1.CoordinatorMessage, error) {
	for {
		if c.link.blocked.Load() {
			select {
			case <-c.link.waitUnblocked():
			case <-c.link.ctx.Done():
				return nil, c.link.ctx.Err()
			}
			continue
		}
		select {
		case m, ok := <-c.in:
			if !ok {
				return nil, io.EOF
			}
			return m, nil
		case <-c.link.waitUnblocked():
			// Blocked while waiting: go round again and wait for the link to come back.
		case <-c.link.ctx.Done():
			return nil, c.link.ctx.Err()
		}
	}
}

func (c *blockableConn) Header() (metadata.MD, error) { return nil, nil }
func (c *blockableConn) Trailer() metadata.MD         { return nil }
func (c *blockableConn) CloseSend() error             { return nil }
func (c *blockableConn) Context() context.Context     { return c.link.ctx }
func (c *blockableConn) SendMsg(m any) error          { return errors.New("unused") }
func (c *blockableConn) RecvMsg(m any) error          { return errors.New("unused") }

var _ grpc.BidiStreamingClient[lazycakev1.AgentMessage, lazycakev1.CoordinatorMessage] = (*blockableConn)(nil)

type blockableClient struct{ stream *blockableStream }

func (c *blockableClient) Connect(ctx context.Context, _ ...grpc.CallOption) (lazycakev1.AgentService_ConnectClient, error) {
	return &blockableConn{link: c.stream, in: make(chan *lazycakev1.CoordinatorMessage, 32)}, nil
}

var _ lazycakev1.AgentServiceClient = (*blockableClient)(nil)

// reportSpy sits between the executor and the runner and notes the first time a task's final report finds no connection to go out
// on, so the test can tell that the report really was made while the agent was cut off, not just after it had reconnected.
type reportSpy struct {
	runner  *conn.Runner
	noLink  chan struct{} // closed the first time a TaskFinished could not be queued
	closeIt sync.Once
}

func (s *reportSpy) Send(m *lazycakev1.AgentMessage) { s.TrySend(m) }

func (s *reportSpy) TrySend(m *lazycakev1.AgentMessage) bool {
	ok := s.runner.TrySend(m)
	if !ok && m.GetFinished() != nil {
		s.closeIt.Do(func() { close(s.noLink) })
	}
	return ok
}

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
	spy := &reportSpy{runner: runner, noLink: make(chan struct{})}
	executor := &lcexec.Executor{
		Runtime: rt, Ledger: ledger, Send: spy, Log: discardLog(),
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
	containerName := "lazycake-" + taskID + "-0"
	t.Cleanup(func() { _ = rt.Remove(context.Background(), containerName) })
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

	// The container really is running: the check that it later stops means nothing unless this one holds.
	pid, err := rt.Pid(ctx, containerName)
	require.NoError(t, err)
	require.Positive(t, pid, "the task's container is not running before the connection is cut")

	blockedAt := time.Now()
	stream.block()

	// The agent can no longer hear the coordinator. Its container must die on its own: lease_s(3) to notice + up to 10s of SIGTERM
	// grace (a bare `sh -c sleep`, as PID 1, ignores SIGTERM by default, so this really does run the full grace period before
	// SIGKILL) + a few seconds of slack for watcher polling and podman's own round trip. This is the guarantee that matters while
	// the network is down: nothing the agent can't report on is left running.
	require.Eventually(t, func() bool {
		pid, err := rt.Pid(ctx, containerName)
		return err != nil || pid == 0
	}, 16*time.Second, 200*time.Millisecond, "the container was still running after lease_s(3)+10s+slack with no contact from the coordinator")

	// The agent also noticed the silence (three heartbeats without an answer) and gave up on that connection, so the report that the
	// task was fenced has nowhere to go. It must not be lost: it goes out when the agent registers again, which it can once the
	// network is back.
	select {
	case <-spy.noLink:
	case <-time.After(10 * time.Second):
		t.Fatal("the fenced task's final report was never made, or it found a connection: the test is not exercising the replay")
	}
	stream.unblock()

	finished := waitForBody(t, stream, 20*time.Second, func(b *lazycakev1.AgentMessage) bool {
		f, ok := b.GetBody().(*lazycakev1.AgentMessage_Finished)
		return ok && f.Finished.GetTaskId() == taskID
	})
	require.NotNil(t, finished, "the fenced task was never reported once the connection came back")

	f := finished.GetBody().(*lazycakev1.AgentMessage_Finished).Finished
	require.Equal(t, "fenced", f.GetExitReason())
	require.Less(t, time.Since(blockedAt), 35*time.Second)
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
