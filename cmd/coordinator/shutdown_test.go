package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// blockingAgentService holds every Connect stream open until the client
// drops it - what a real agent's stream looks like to the server.
type blockingAgentService struct {
	lazycakev1.UnimplementedAgentServiceServer
	connected chan struct{}
}

func (b *blockingAgentService) Connect(stream lazycakev1.AgentService_ConnectServer) error {
	close(b.connected)
	<-stream.Context().Done()
	return nil
}

// A coordinator restart must not wait on agents that keep their stream open:
// stopGRPC returns once its timeout lapses, with the stream still up.
func TestStopGRPCDoesNotWaitForeverOnOpenStreams(t *testing.T) {
	svc := &blockingAgentService{connected: make(chan struct{})}
	srv := grpc.NewServer()
	lazycakev1.RegisterAgentServiceServer(srv, svc)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(lis)

	cc, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := lazycakev1.NewAgentServiceClient(cc).Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&lazycakev1.AgentMessage{}); err != nil { // makes the stream reach the handler
		t.Fatal(err)
	}
	select {
	case <-svc.connected:
	case <-time.After(3 * time.Second):
		t.Fatal("handler never started")
	}

	start := time.Now()
	stopGRPC(srv, 300*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("stopGRPC took %v with a stream open; it should give up after its timeout", elapsed)
	}
}

// With nothing in flight it returns at once, not after the timeout.
func TestStopGRPCIsQuickWhenIdle(t *testing.T) {
	srv := grpc.NewServer()
	lis, _ := net.Listen("tcp", "127.0.0.1:0")
	go srv.Serve(lis)
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	stopGRPC(srv, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("idle stop took %v", elapsed)
	}
}
