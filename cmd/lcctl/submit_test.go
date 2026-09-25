package main

import (
	"context"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/pricing"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// fakeStore is the same minimal in-memory store.Store double the api
// package's own tests use - reimplemented here (rather than exported from
// api, which shouldn't export test scaffolding) since this is the
// smallest slice cmd/lcctl's own test actually needs.
type fakeStore struct {
	store.Store
	mu       sync.Mutex
	accounts map[string]store.Account
	tokens   map[string]store.APIToken
	tasks    map[string]store.Task
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		accounts: map[string]store.Account{},
		tokens:   map[string]store.APIToken{},
		tasks:    map[string]store.Task{},
	}
}

func (f *fakeStore) Authenticate(ctx context.Context, tokenHash []byte) (store.APIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tok, ok := f.tokens[string(tokenHash)]
	if !ok {
		return store.APIToken{}, store.ErrNotFound
	}
	return tok, nil
}

func (f *fakeStore) GetGateway(ctx context.Context, id string) (store.Gateway, error) {
	return store.Gateway{}, store.ErrNotFound
}

func (f *fakeStore) AvailableBalance(ctx context.Context, accountID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.accounts[accountID]
	if !ok {
		return 0, store.ErrNotFound
	}
	return a.BalanceMicros, nil
}

func (f *fakeStore) CreateTask(ctx context.Context, t store.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[t.ID] = t
	return nil
}

// TestSubmitFanoutHitsRealServer is task 6.3's own verify, "a single lcctl
// command fans out N tasks across the fleet": submitFanout, driven through
// a real gRPC round trip against api.CustomerServer (bufconn, not a fake
// client), creates exactly count distinct task rows and prints every task
// ID it got back.
func TestSubmitFanoutHitsRealServer(t *testing.T) {
	fs := newFakeStore()
	fs.accounts["act_1"] = store.Account{ID: "act_1", BalanceMicros: 1_000_000_000}
	fs.tokens[string(auth.Hash("cust"))] = store.APIToken{AccountID: "act_1", Kind: store.TokenCustomer}

	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	lazycakev1.RegisterCustomerServiceServer(grpcServer, &api.CustomerServer{Store: fs, Rates: pricing.DefaultRates()})
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
	client := lazycakev1.NewCustomerServiceClient(conn)

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer cust"))
	base := &lazycakev1.SubmitTaskRequest{
		Image:  "docker.io/library/refworkload@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Limits: &lazycakev1.TaskLimits{CpuCores: 0.5, MemoryMb: 128, DiskMb: 256, WallTimeoutS: 30},
	}

	const count = 50
	if err := submitFanout(ctx, client, base, "chaos", count); err != nil {
		t.Fatalf("submitFanout: %v", err)
	}

	if got := len(fs.tasks); got != count {
		t.Fatalf("created %d task rows, want %d", got, count)
	}

	seenKeys := map[string]bool{}
	for _, task := range fs.tasks {
		if task.IdempotencyKey == nil {
			t.Fatal("expected every fanned-out task to have an idempotency key")
		}
		if seenKeys[*task.IdempotencyKey] {
			t.Fatalf("duplicate idempotency key %q across fanned-out tasks", *task.IdempotencyKey)
		}
		seenKeys[*task.IdempotencyKey] = true
	}
}
