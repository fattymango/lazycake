package api

import (
	"context"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// fakeGatewayEvents records every RecordGatewayBytes call, so the test can
// assert the RPC actually reached billing rather than just returning 200.
type fakeGatewayEvents struct {
	mu    sync.Mutex
	calls []struct {
		taskID string
		bytes  int64
	}
}

func (f *fakeGatewayEvents) RecordGatewayBytes(taskID string, bytes int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		taskID string
		bytes  int64
	}{taskID, bytes})
}

func startGatewayTestServer(t *testing.T, fs *fakeStore, events GatewayEvents) lazycakev1.GatewayServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	lazycakev1.RegisterGatewayServiceServer(grpcServer, &GatewayServer{Store: fs, Events: events})
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
	return lazycakev1.NewGatewayServiceClient(conn)
}

func authedCtx(token string) context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}

// TestGatewayReportBytes proves the live wiring task 4.3 added: a real
// gRPC round trip, authenticated with the gateway's own bearer token,
// ownership-checked against the store, and the byte counts reaching
// billing's GatewayEvents.
func TestGatewayReportBytes(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("gw-token")), store.APIToken{AccountID: "act_1", Kind: store.TokenGateway})
	if err := fs.CreateGateway(context.Background(), store.Gateway{ID: "gw_1", AccountID: "act_1"}); err != nil {
		t.Fatalf("CreateGateway: %v", err)
	}
	events := &fakeGatewayEvents{}
	client := startGatewayTestServer(t, fs, events)

	_, err := client.ReportBytes(authedCtx("gw-token"), &lazycakev1.ByteReport{
		GatewayId: "gw_1", TaskId: "tsk_1", BytesToLocal: 1000, BytesToTask: 500,
	})
	if err != nil {
		t.Fatalf("ReportBytes: %v", err)
	}

	events.mu.Lock()
	defer events.mu.Unlock()
	if len(events.calls) != 1 {
		t.Fatalf("expected 1 RecordGatewayBytes call, got %d", len(events.calls))
	}
	if events.calls[0].taskID != "tsk_1" || events.calls[0].bytes != 1500 {
		t.Fatalf("unexpected call: %+v", events.calls[0])
	}
}

func TestGatewayReportBytesRejectsWrongAccount(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("gw-token")), store.APIToken{AccountID: "act_attacker", Kind: store.TokenGateway})
	if err := fs.CreateGateway(context.Background(), store.Gateway{ID: "gw_1", AccountID: "act_owner"}); err != nil {
		t.Fatalf("CreateGateway: %v", err)
	}
	events := &fakeGatewayEvents{}
	client := startGatewayTestServer(t, fs, events)

	_, err := client.ReportBytes(authedCtx("gw-token"), &lazycakev1.ByteReport{
		GatewayId: "gw_1", TaskId: "tsk_1", BytesToLocal: 1000, BytesToTask: 500,
	})
	if err == nil {
		t.Fatal("expected an error reporting bytes for a gateway owned by a different account")
	}

	events.mu.Lock()
	defer events.mu.Unlock()
	if len(events.calls) != 0 {
		t.Fatalf("expected no RecordGatewayBytes call for a rejected report, got %d", len(events.calls))
	}
}

func TestGatewayReportBytesRejectsNonGatewayToken(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust-token")), store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer})
	if err := fs.CreateGateway(context.Background(), store.Gateway{ID: "gw_1", AccountID: "act_1"}); err != nil {
		t.Fatalf("CreateGateway: %v", err)
	}
	client := startGatewayTestServer(t, fs, &fakeGatewayEvents{})

	_, err := client.ReportBytes(authedCtx("cust-token"), &lazycakev1.ByteReport{
		GatewayId: "gw_1", TaskId: "tsk_1",
	})
	if err == nil {
		t.Fatal("expected an error reporting bytes with a non-gateway token")
	}
}
