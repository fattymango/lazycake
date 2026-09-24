package api

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func startTestServer(t *testing.T, fs *fakeStore) (lazycakev1.AgentServiceClient, func()) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	srv := &Server{
		Store:      fs,
		Registry:   NewRegistry(),
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		HeartbeatS: 15,
		LeaseS:     60,
	}
	lazycakev1.RegisterAgentServiceServer(grpcServer, srv)

	go grpcServer.Serve(lis)
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dialing bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return lazycakev1.NewAgentServiceClient(conn), grpcServer.Stop
}

func TestConnectRegisterAndHeartbeat(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("good-token")), store.APIToken{AccountID: "act_1", Kind: store.TokenAgent})

	client, _ := startTestServer(t, fs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Register{
		Register: &lazycakev1.Register{Token: "good-token", Hostname: "host1", Arch: "amd64"},
	}}); err != nil {
		t.Fatalf("send register: %v", err)
	}

	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv register ack: %v", err)
	}
	ack := msg.GetRegisterAck()
	if ack == nil || ack.GetNodeId() == "" {
		t.Fatalf("expected RegisterAck with node id, got %+v", msg)
	}
	nodeID := ack.GetNodeId()

	if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Heartbeat{
		Heartbeat: &lazycakev1.Heartbeat{Seq: 1},
	}}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}
	msg, err = stream.Recv()
	if err != nil {
		t.Fatalf("recv heartbeat ack: %v", err)
	}
	if msg.GetHeartbeatAck() == nil || msg.GetHeartbeatAck().GetSeq() != 1 {
		t.Fatalf("expected HeartbeatAck seq=1, got %+v", msg)
	}

	n, err := fs.GetNode(context.Background(), nodeID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if !n.Connected || n.Hostname != "host1" {
		t.Fatalf("unexpected node state: %+v", n)
	}
}

func TestConnectRejectsBadToken(t *testing.T) {
	fs := newFakeStore()
	client, _ := startTestServer(t, fs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Register{
		Register: &lazycakev1.Register{Token: "bad-token"},
	}}); err != nil {
		t.Fatalf("send register: %v", err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatal("expected error for bad token")
	}
}
