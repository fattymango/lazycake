package conn

import (
	"context"
	"errors"
	"io"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// fakeStream is a hand-written double for the generated bidi-streaming
// client interface, driven entirely by channels so tests can script exactly
// what the "coordinator" sends and assert exactly what the agent sent.
type fakeStream struct {
	ctx  context.Context
	in   chan *lazycakev1.CoordinatorMessage // messages the fake coordinator sends
	out  chan *lazycakev1.AgentMessage       // messages the fake stream received from the agent
	recv chan error                          // pending errors to hand back from Recv, if any
}

func newFakeStream(ctx context.Context) *fakeStream {
	return &fakeStream{
		ctx:  ctx,
		in:   make(chan *lazycakev1.CoordinatorMessage, 32),
		out:  make(chan *lazycakev1.AgentMessage, 32),
		recv: make(chan error, 1),
	}
}

func (f *fakeStream) Send(m *lazycakev1.AgentMessage) error {
	select {
	case f.out <- m:
		return nil
	case <-f.ctx.Done():
		return f.ctx.Err()
	}
}

func (f *fakeStream) Recv() (*lazycakev1.CoordinatorMessage, error) {
	select {
	case err := <-f.recv:
		return nil, err
	case m, ok := <-f.in:
		if !ok {
			return nil, io.EOF
		}
		return m, nil
	case <-f.ctx.Done():
		return nil, f.ctx.Err()
	}
}

func (f *fakeStream) failRecv(err error) { f.recv <- err }

func (f *fakeStream) Header() (metadata.MD, error) { return nil, nil }
func (f *fakeStream) Trailer() metadata.MD         { return nil }
func (f *fakeStream) CloseSend() error             { return nil }
func (f *fakeStream) Context() context.Context     { return f.ctx }
func (f *fakeStream) SendMsg(m any) error          { return errors.New("unused") }
func (f *fakeStream) RecvMsg(m any) error          { return errors.New("unused") }

var _ grpc.BidiStreamingClient[lazycakev1.AgentMessage, lazycakev1.CoordinatorMessage] = (*fakeStream)(nil)

// fakeClient hands out a fresh fakeStream on each Connect call, and counts
// how many times it was called, so reconnect tests can assert the runner
// actually redialed.
type fakeClient struct {
	connects int32
	next     func(ctx context.Context) *fakeStream
}

func (c *fakeClient) Connect(ctx context.Context, _ ...grpc.CallOption) (lazycakev1.AgentService_ConnectClient, error) {
	atomic.AddInt32(&c.connects, 1)
	return c.next(ctx), nil
}

func (c *fakeClient) connectCount() int { return int(atomic.LoadInt32(&c.connects)) }

var _ lazycakev1.AgentServiceClient = (*fakeClient)(nil)
