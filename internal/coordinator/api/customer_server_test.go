package api

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func startCustomerTestServer(t *testing.T, fs *fakeStore) lazycakev1.CustomerServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	lazycakev1.RegisterCustomerServiceServer(grpcServer, &CustomerServer{Store: fs})
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
	return lazycakev1.NewCustomerServiceClient(conn)
}

func authCtx(token string) context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}

func TestSubmitTaskRequiresDigestPinnedImage(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust")), store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer})
	client := startCustomerTestServer(t, fs)

	_, err := client.SubmitTask(authCtx("cust"), &lazycakev1.SubmitTaskRequest{
		Image:  "alpine:latest", // no digest
		Limits: &lazycakev1.TaskLimits{CpuCores: 1, MemoryMb: 256, DiskMb: 512, WallTimeoutS: 30},
	})
	if err == nil {
		t.Fatal("expected error for non-digest-pinned image")
	}
}

func TestSubmitTaskRejectsTooManyGateways(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust")), store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer})
	client := startCustomerTestServer(t, fs)

	_, err := client.SubmitTask(authCtx("cust"), &lazycakev1.SubmitTaskRequest{
		Image:      "alpine@sha256:abc",
		Limits:     &lazycakev1.TaskLimits{CpuCores: 1, MemoryMb: 256, DiskMb: 512, WallTimeoutS: 30},
		GatewayIds: []string{"gw_1", "gw_2", "gw_3", "gw_4"},
	})
	if err == nil {
		t.Fatal("expected error for more than 3 gateways")
	}
}

func TestSubmitTaskRejectsUnknownGateway(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust")), store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer})
	client := startCustomerTestServer(t, fs)

	_, err := client.SubmitTask(authCtx("cust"), &lazycakev1.SubmitTaskRequest{
		Image:      "alpine@sha256:abc",
		Limits:     &lazycakev1.TaskLimits{CpuCores: 1, MemoryMb: 256, DiskMb: 512, WallTimeoutS: 30},
		GatewayIds: []string{"gw_nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error for an unknown gateway id")
	}
}

func TestSubmitTaskRejectsOtherAccountsGateway(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust1")), store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer})
	fs.gateways["gw_other"] = store.Gateway{ID: "gw_other", AccountID: "act_2", Label: "not yours"}
	client := startCustomerTestServer(t, fs)

	_, err := client.SubmitTask(authCtx("cust1"), &lazycakev1.SubmitTaskRequest{
		Image:      "alpine@sha256:abc",
		Limits:     &lazycakev1.TaskLimits{CpuCores: 1, MemoryMb: 256, DiskMb: 512, WallTimeoutS: 30},
		GatewayIds: []string{"gw_other"},
	})
	if err == nil {
		t.Fatal("expected error submitting with another account's gateway")
	}
}

func TestCreateAndListGateways(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust")), store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer})
	client := startCustomerTestServer(t, fs)

	created, err := client.CreateGateway(authCtx("cust"), &lazycakev1.CreateGatewayRequest{
		Label: "prod-db", Services: []string{"db:5432"},
	})
	if err != nil {
		t.Fatalf("CreateGateway: %v", err)
	}
	if created.GetGatewayId() == "" || created.GetInstallToken() == "" {
		t.Fatalf("expected a gateway id and install token, got %+v", created)
	}

	listed, err := client.ListGateways(authCtx("cust"), &lazycakev1.ListGatewaysRequest{})
	if err != nil {
		t.Fatalf("ListGateways: %v", err)
	}
	if len(listed.GetGateways()) != 1 || listed.GetGateways()[0].GetGatewayId() != created.GetGatewayId() {
		t.Fatalf("expected the created gateway to be listed, got %+v", listed.GetGateways())
	}

	// The install token authenticates as a gateway token for that account.
	tok, err := fs.Authenticate(context.Background(), auth.Hash(created.GetInstallToken()))
	if err != nil {
		t.Fatalf("install token does not authenticate: %v", err)
	}
	if tok.Kind != store.TokenGateway || tok.AccountID != "act_1" {
		t.Fatalf("unexpected token: %+v", tok)
	}
}

func TestSubmitAndGetTask(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust")), store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer})
	client := startCustomerTestServer(t, fs)

	resp, err := client.SubmitTask(authCtx("cust"), &lazycakev1.SubmitTaskRequest{
		Image:  "alpine@sha256:abc",
		Limits: &lazycakev1.TaskLimits{CpuCores: 1, MemoryMb: 256, DiskMb: 512, WallTimeoutS: 30},
	})
	if err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	if resp.GetTaskId() == "" {
		t.Fatal("expected a task id")
	}

	status, err := client.GetTask(authCtx("cust"), &lazycakev1.GetTaskRequest{TaskId: resp.GetTaskId()})
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if status.GetState() != string(store.TaskQueued) {
		t.Fatalf("expected queued, got %s", status.GetState())
	}
}

func TestGetTaskOtherAccountNotFound(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust1")), store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer})
	fs.addToken(string(auth.Hash("cust2")), store.APIToken{AccountID: "act_2", Kind: store.TokenCustomer})
	client := startCustomerTestServer(t, fs)

	resp, err := client.SubmitTask(authCtx("cust1"), &lazycakev1.SubmitTaskRequest{
		Image:  "alpine@sha256:abc",
		Limits: &lazycakev1.TaskLimits{CpuCores: 1, MemoryMb: 256, DiskMb: 512, WallTimeoutS: 30},
	})
	if err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}

	_, err = client.GetTask(authCtx("cust2"), &lazycakev1.GetTaskRequest{TaskId: resp.GetTaskId()})
	if err == nil {
		t.Fatal("expected not-found reading another account's task")
	}
}

func TestStreamLogsNoFollow(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust")), store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer})
	client := startCustomerTestServer(t, fs)

	resp, err := client.SubmitTask(authCtx("cust"), &lazycakev1.SubmitTaskRequest{
		Image:  "alpine@sha256:abc",
		Limits: &lazycakev1.TaskLimits{CpuCores: 1, MemoryMb: 256, DiskMb: 512, WallTimeoutS: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	fs.logs = append(fs.logs,
		store.LogLine{TaskID: resp.GetTaskId(), Seq: 1, Stream: "stdout", At: time.Now(), Line: "hi"},
		store.LogLine{TaskID: resp.GetTaskId(), Seq: 2, Stream: "stdout", At: time.Now(), Line: "bye"},
	)

	stream, err := client.StreamLogs(authCtx("cust"), &lazycakev1.StreamLogsRequest{TaskId: resp.GetTaskId(), Follow: false})
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for {
		l, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, l.GetLine())
	}
	if len(lines) != 2 || lines[0] != "hi" || lines[1] != "bye" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}
