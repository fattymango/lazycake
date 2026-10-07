package api

import (
	"context"
	"errors"
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

// A heartbeat's usage reading is stored (task 8.15), sanitised, and a failure to store it
// never fails the heartbeat (which is what keeps the node's lease alive).
func TestHeartbeatUsageIsStoredSanitisedAndNeverBreaksTheHeartbeat(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("good-token")), store.APIToken{AccountID: "act_1", Kind: store.TokenAgent})
	client, _ := startTestServer(t, fs)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Register{
		Register: &lazycakev1.Register{Token: "good-token", Hostname: "host1", Arch: "amd64"},
	}}); err != nil {
		t.Fatal(err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := msg.GetRegisterAck().GetNodeId()

	beat := func(seq int64, u *lazycakev1.UsageSample) {
		t.Helper()
		if err := stream.Send(&lazycakev1.AgentMessage{Body: &lazycakev1.AgentMessage_Heartbeat{Heartbeat: &lazycakev1.Heartbeat{Seq: seq, Usage: u}}}); err != nil {
			t.Fatal(err)
		}
		ack, err := stream.Recv()
		if err != nil || ack.GetHeartbeatAck().GetSeq() != seq {
			t.Fatalf("heartbeat %d was not acked: %v %+v", seq, err, ack)
		}
	}

	var tooMany []*lazycakev1.TaskUsage
	for i := 0; i < maxUsageTasks+5; i++ {
		tooMany = append(tooMany, &lazycakev1.TaskUsage{TaskId: "tsk_x", CpuCores: 1})
	}
	tooMany[0].CpuCores = -3
	tooMany[0].MemoryBytes = -1
	beat(1, &lazycakev1.UsageSample{IntervalMs: 15000, HostCpuBusy: 7.5, HostCpuCount: 4, HostMemTotalBytes: 100, HostMemUsedBytes: -5, Tasks: tooMany})
	beat(2, nil) // an old agent: no usage at all

	fs.mu.Lock()
	fs.usageErr = errors.New("database is down")
	fs.mu.Unlock()
	beat(3, &lazycakev1.UsageSample{HostCpuCount: 2}) // storing fails, the heartbeat still succeeds

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if len(fs.usage) != 2 {
		t.Fatalf("want 2 stored readings (the old agent's heartbeat has none), got %d", len(fs.usage))
	}
	got := fs.usage[0]
	if got.NodeID != nodeID || got.HostCPUBusy != 1 || got.HostMemUsed != 0 || got.HostCPUCount != 4 {
		t.Fatalf("reading not sanitised: %+v", got)
	}
	if len(got.Tasks) != maxUsageTasks {
		t.Fatalf("task list not capped: %d", len(got.Tasks))
	}
	if got.Tasks[0].CPUCores != 0 || got.Tasks[0].MemoryBytes != 0 {
		t.Fatalf("negative task figures not clamped: %+v", got.Tasks[0])
	}
}
