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

// Traffic statistics (task 8.14): what a gateway reports is kept per task and per
// gateway, deltas add up, and a gateway can't attribute traffic to someone else's task.
func TestGatewayReportBytesKeepsTrafficStatistics(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("gw-token")), store.APIToken{AccountID: "act_1", Kind: store.TokenGateway})
	if err := fs.CreateGateway(context.Background(), store.Gateway{ID: "gw_1", AccountID: "act_1"}); err != nil {
		t.Fatal(err)
	}
	// One task of the gateway's own account, one belonging to a stranger.
	fs.tasks["tsk_mine"] = store.Task{ID: "tsk_mine", AccountID: "act_1"}
	fs.tasks["tsk_theirs"] = store.Task{ID: "tsk_theirs", AccountID: "act_other"}
	client := startGatewayTestServer(t, fs, &fakeGatewayEvents{})
	report := func(task string, toLocal, toTask int64, svc string, final bool) {
		t.Helper()
		if _, err := client.ReportBytes(authedCtx("gw-token"), &lazycakev1.ByteReport{GatewayId: "gw_1", TaskId: task, BytesToLocal: toLocal, BytesToTask: toTask, Service: svc, Final: final}); err != nil {
			t.Fatalf("ReportBytes: %v", err)
		}
	}

	report("tsk_mine", 10, 100, "db", false) // progress
	report("tsk_mine", 5, 50, "db", false)   // progress
	report("tsk_mine", 1, 1, "db", true)     // close
	report("tsk_theirs", 999, 999, "db", true)
	report("tsk_mine", 7, 7, "", false) // a gateway built before these fields: no service

	totals, _ := fs.GatewayTotals(context.Background(), []string{"gw_1"})
	if got := totals["gw_1"]; got.BytesToLocal != 10+5+1+999+7 || got.BytesToTask != 100+50+1+999+7 {
		t.Fatalf("gateway totals wrong (every report counts toward the gateway): %+v", got)
	}
	if totals["gw_1"].Connections != 3 {
		t.Fatalf("want 3 connections (the closed one, the stranger's, and the legacy report counts as a whole connection), got %d", totals["gw_1"].Connections)
	}

	mine, _ := fs.TaskGatewayUsage(context.Background(), "tsk_mine")
	var sum int64
	for _, r := range mine {
		sum += r.BytesToLocal
	}
	if sum != 10+5+1+7 {
		t.Fatalf("the task's own usage should be its deltas summed, got %d (%+v)", sum, mine)
	}
	theirs, _ := fs.TaskGatewayUsage(context.Background(), "tsk_theirs")
	if len(theirs) != 0 {
		t.Fatalf("a gateway must not be able to attribute traffic to another account's task: %+v", theirs)
	}
}

func TestGatewayReportBytesRejectsNegativeCounts(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("gw-token")), store.APIToken{AccountID: "act_1", Kind: store.TokenGateway})
	_ = fs.CreateGateway(context.Background(), store.Gateway{ID: "gw_1", AccountID: "act_1"})
	events := &fakeGatewayEvents{}
	client := startGatewayTestServer(t, fs, events)

	if _, err := client.ReportBytes(authedCtx("gw-token"), &lazycakev1.ByteReport{GatewayId: "gw_1", TaskId: "tsk_1", BytesToLocal: -5, BytesToTask: 10}); err == nil {
		t.Fatal("a negative byte count must be rejected, or it could subtract from the totals")
	}
	if len(events.calls) != 0 {
		t.Fatal("a rejected report must not reach billing either")
	}
}

// A batched report (task: many tiny connections, one report) counts every connection it closes; an older
// gateway's final report still counts as exactly one.
func TestGatewayReportBytesCountsBatchedConnections(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("gw-token")), store.APIToken{AccountID: "act_1", Kind: store.TokenGateway})
	_ = fs.CreateGateway(context.Background(), store.Gateway{ID: "gw_1", AccountID: "act_1"})
	fs.tasks["tsk_a"] = store.Task{ID: "tsk_a", AccountID: "act_1"}
	client := startGatewayTestServer(t, fs, &fakeGatewayEvents{})
	send := func(r *lazycakev1.ByteReport) {
		t.Helper()
		r.GatewayId = "gw_1"
		if _, err := client.ReportBytes(authedCtx("gw-token"), r); err != nil {
			t.Fatal(err)
		}
	}
	send(&lazycakev1.ByteReport{TaskId: "tsk_a", Service: "db", BytesToLocal: 10, Final: true, Connections: 250}) // batched
	send(&lazycakev1.ByteReport{TaskId: "tsk_a", Service: "db", BytesToLocal: 1, Final: true})                    // an older gateway: one
	send(&lazycakev1.ByteReport{TaskId: "tsk_a", Service: "db", BytesToLocal: 1, Connections: -5, Final: true})   // nonsense count: treated as one
	send(&lazycakev1.ByteReport{TaskId: "tsk_a", Service: "db", BytesToLocal: 1})                                 // still open: none

	totals, _ := fs.GatewayTotals(context.Background(), []string{"gw_1"})
	if got := totals["gw_1"].Connections; got != 252 {
		t.Fatalf("connections = %d, want 250 + 1 + 1 + 0 = 252", got)
	}
	if got := totals["gw_1"].BytesToLocal; got != 13 {
		t.Fatalf("bytes must still add up: %d", got)
	}
}
