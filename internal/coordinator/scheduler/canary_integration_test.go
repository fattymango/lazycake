//go:build integration

package scheduler

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// fakeCanaryStore is a minimal store.Store double covering just what
// api.GatewayServer.ReportBytes touches: Authenticate, GetGateway, and the
// traffic bookkeeping (which this test doesn't look at).
type fakeCanaryStore struct {
	store.Store
	tokens   map[string]store.APIToken
	gateways map[string]store.Gateway
}

func (f *fakeCanaryStore) Authenticate(ctx context.Context, tokenHash []byte) (store.APIToken, error) {
	tok, ok := f.tokens[string(tokenHash)]
	if !ok {
		return store.APIToken{}, store.ErrNotFound
	}
	return tok, nil
}

func (f *fakeCanaryStore) GetGateway(ctx context.Context, id string) (store.Gateway, error) {
	gw, ok := f.gateways[id]
	if !ok {
		return store.Gateway{}, store.ErrNotFound
	}
	return gw, nil
}

func (f *fakeCanaryStore) GetTask(ctx context.Context, id string) (store.Task, error) {
	return store.Task{}, store.ErrNotFound
}

func (f *fakeCanaryStore) RecordGatewayTraffic(ctx context.Context, r store.GatewayTrafficReport) error {
	return nil
}

// canaryFanout is exactly cmd/coordinator's gatewayFanout, minus the
// reconciler half - this test only cares about canary detection.
type canaryFanout struct{ canaries *CanaryTracker }

func (f canaryFanout) RecordGatewayBytes(taskID string, bytes int64) {
	f.canaries.RecordGatewayActivity(taskID)
}

func startCanaryGatewayServer(t *testing.T, fs *fakeCanaryStore, events api.GatewayEvents) lazycakev1.GatewayServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	lazycakev1.RegisterGatewayServiceServer(grpcServer, &api.GatewayServer{Store: fs, Events: events})
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

func canaryAuthedCtx(token string) context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
}

func discardCanaryLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestCanaryDetection is task 5.2's own verify: a node returning early
// without running the canary's workload is caught by the platform gateway
// seeing no connection - proven with a real gRPC round trip through
// api.GatewayServer.ReportBytes for the "connected" case, and its
// deliberate absence for the "never connected" case, driving
// CanaryTracker exactly as cmd/coordinator wires it end to end
// (GatewayServer -> CanaryTracker.RecordGatewayActivity -> resolved
// against TrustTracker).
func TestCanaryDetection(t *testing.T) {
	t.Run("a node that actually connects passes", func(t *testing.T) {
		fs := &fakeCanaryStore{
			tokens:   map[string]store.APIToken{string(auth.Hash("gw-token")): {AccountID: "act_platform", Kind: store.TokenGateway}},
			gateways: map[string]store.Gateway{"gw_platform": {ID: "gw_platform", AccountID: "act_platform"}},
		}
		trust := NewTrustTracker()
		canaries := NewCanaryTracker(trust, discardCanaryLog())
		client := startCanaryGatewayServer(t, fs, canaryFanout{canaries})

		before := trust.Score("nod_honest")
		canaries.Expect("tsk_canary_ok", "nod_honest", 0) // resolves after canaryGrace (5s)

		if _, err := client.ReportBytes(canaryAuthedCtx("gw-token"), &lazycakev1.ByteReport{
			GatewayId: "gw_platform", TaskId: "tsk_canary_ok", BytesToLocal: 10, BytesToTask: 10,
		}); err != nil {
			t.Fatalf("ReportBytes: %v", err)
		}

		time.Sleep(6 * time.Second) // past canaryGrace
		after := trust.Score("nod_honest")
		if after <= before {
			t.Fatalf("trust score did not rise for a canary that connected: %v -> %v", before, after)
		}
	})

	t.Run("a node that returns early without connecting is caught", func(t *testing.T) {
		trust := NewTrustTracker()
		canaries := NewCanaryTracker(trust, discardCanaryLog())
		// No gateway server call at all for this task_id - simulating the
		// node reporting the task "finished" without the container ever
		// actually reaching the platform gateway.

		before := trust.Score("nod_cheater")
		canaries.Expect("tsk_canary_miss", "nod_cheater", 0)

		time.Sleep(6 * time.Second) // past canaryGrace
		after := trust.Score("nod_cheater")
		if after >= before {
			t.Fatalf("trust score did not drop for a canary the gateway never saw: %v -> %v", before, after)
		}
		// A single canary failure (-0.30) from the 0.5 start lands exactly
		// on the 0.2 ban threshold - not (yet) banned on its own, since
		// Banned is a strict less-than, but a second one would be.
		if got, want := after, TrustStart-trustCanaryFail; got != want {
			t.Fatalf("score after one missed canary = %v, want %v", got, want)
		}
	})
}
