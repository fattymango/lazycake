//go:build integration

// Package e2e exercises the coordinator and agent together, in-process,
// against a real Postgres and a real container engine. It is the automated
// form of IMPLEMENTATION.md task 1.8's verify command: submit a task via
// the store, watch it reach succeeded with the right exit code end to end
// through real gRPC, real dispatch, and a real container.
//
// This package intentionally imports both internal/coordinator/... and
// internal/agent/... - no production binary does that (cmd/coordinator and
// cmd/agent each depend on one side only), but a whole-system test has to
// stand up both sides to be worth anything.
package e2e

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mkassab215/lazycake/internal/agent/capacity"
	"github.com/mkassab215/lazycake/internal/agent/conn"
	lcexec "github.com/mkassab215/lazycake/internal/agent/exec"
	lcruntime "github.com/mkassab215/lazycake/internal/agent/runtime"
	"github.com/mkassab215/lazycake/internal/clock"
	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/scheduler"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/id"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

const probeImage = "docker.io/library/alpine:latest"

func testStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	url := os.Getenv("LAZYCAKE_TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://lazycake:lazycake@localhost:5432/lazycake?sslmode=disable"
	}
	ctx := context.Background()
	s, err := store.NewPostgresStore(ctx, url)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	_, err = s.Pool().Exec(ctx, `TRUNCATE task_logs, node_images, tasks, nodes, api_tokens, accounts CASCADE`)
	require.NoError(t, err)
	return s
}

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// harness stands up a coordinator (bufconn gRPC + scheduler) and one agent
// against it, both real, and returns the store to assert against plus a
// cancel func to tear everything down.
type harness struct {
	st    *store.PostgresStore
	token string
}

func startHarness(t *testing.T) (*harness, context.Context) {
	t.Helper()
	st := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	require.NoError(t, st.CreateAccount(ctx, store.Account{ID: "act_e2e", Name: "e2e"}))
	agentToken := "e2e-agent-token"
	require.NoError(t, st.CreateToken(ctx, store.APIToken{
		TokenHash: auth.Hash(agentToken), AccountID: "act_e2e", Kind: store.TokenAgent,
	}))

	lis := bufconn.Listen(1024 * 1024)
	registry := api.NewRegistry()
	sched := scheduler.New(st, registry, clock.Real{}, discardLog(), 60)
	grpcServer := grpc.NewServer()
	lazycakev1.RegisterAgentServiceServer(grpcServer, &api.Server{
		Store: st, Registry: registry, Events: sched, Capacity: sched,
		Clock: clock.Real{}, Log: discardLog(), HeartbeatS: 2, LeaseS: 60,
	})
	go grpcServer.Serve(lis)
	t.Cleanup(grpcServer.Stop)
	go sched.Run(ctx, 200*time.Millisecond)

	clientConn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { clientConn.Close() })

	sock := os.Getenv("LAZYCAKE_TEST_PODMAN_SOCKET")
	if sock == "" {
		sock, err = lcruntime.DefaultSocket()
		require.NoError(t, err)
	}
	rt, err := lcruntime.NewPodmanRuntime(sock)
	require.NoError(t, err)
	t.Cleanup(func() { rt.Close() })

	_, err = rt.Pull(ctx, probeImage)
	require.NoError(t, err)

	ledger := capacity.NewLedger(capacity.Resources{Cores: 2, MemoryMB: 2048, DiskMB: 10000})
	runner := &conn.Runner{
		Client: lazycakev1.NewAgentServiceClient(clientConn),
		Identity: conn.Identity{
			Token: agentToken, Hostname: "e2e-host", Arch: "amd64",
			InstanceID: id.New("ins"),
			Offer:      &lazycakev1.Offer{Cores: 2, MemoryMb: 2048, DiskMb: 10000},
		},
		Log: discardLog(),
	}
	executor := &lcexec.Executor{
		Runtime: rt, Ledger: ledger, Send: runner, Log: discardLog(),
		InstanceID: "ins_e2e", BootID: "boot_e2e",
	}
	runner.Handlers = conn.Handlers{OnDispatch: executor.HandleDispatch, RunningTaskIDs: ledger.TaskIDs}
	go runner.Run(ctx)

	// Wait for the node to actually register before returning.
	require.Eventually(t, func() bool {
		nodes, err := st.ListNodes(ctx)
		return err == nil && len(nodes) == 1 && nodes[0].Connected
	}, 10*time.Second, 100*time.Millisecond, "agent never registered")

	return &harness{st: st, token: agentToken}, ctx
}

func TestDispatchEndToEnd(t *testing.T) {
	h, ctx := startHarness(t)

	taskID := id.New(id.Task)
	require.NoError(t, h.st.CreateTask(ctx, store.Task{
		ID: taskID, AccountID: "act_e2e", State: store.TaskQueued,
		Image:        probeImage,
		Entrypoint:   []string{"sh", "-c"},
		Args:         []string{"echo hello-e2e"},
		Limits:       store.Limits{CPUCores: 1, MemoryMB: 128, DiskMB: 500, WallTimeoutS: 30},
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
	}))

	var final store.Task
	require.Eventually(t, func() bool {
		task, err := h.st.GetTask(ctx, taskID)
		require.NoError(t, err)
		final = task
		return task.State == store.TaskSucceeded || task.State == store.TaskFailed
	}, 30*time.Second, 200*time.Millisecond, "task never finished")

	require.Equal(t, store.TaskSucceeded, final.State)
	require.NotNil(t, final.ExitCode)
	require.Equal(t, 0, *final.ExitCode)
	require.NotNil(t, final.ExitReason)
	require.Equal(t, "exited", *final.ExitReason)
}

func TestDispatchOOM(t *testing.T) {
	h, ctx := startHarness(t)

	taskID := id.New(id.Task)
	require.NoError(t, h.st.CreateTask(ctx, store.Task{
		ID: taskID, AccountID: "act_e2e", State: store.TaskQueued,
		Image:        probeImage,
		Entrypoint:   []string{"sh", "-c"},
		Args:         []string{"dd if=/dev/zero of=/dev/shm/fill bs=1M count=128"},
		Limits:       store.Limits{CPUCores: 1, MemoryMB: 64, DiskMB: 500, WallTimeoutS: 30},
		Requirements: store.Requirements{Arch: "amd64", Isolation: "podman"},
		Delivery:     store.AtMostOnce, Retry: store.Retry{MaxAttempts: 1},
	}))

	var final store.Task
	require.Eventually(t, func() bool {
		task, err := h.st.GetTask(ctx, taskID)
		require.NoError(t, err)
		final = task
		return task.State == store.TaskSucceeded || task.State == store.TaskFailed
	}, 30*time.Second, 200*time.Millisecond, "task never finished")

	require.Equal(t, store.TaskFailed, final.State)
	require.NotNil(t, final.ExitReason)
	require.Equal(t, "oom", *final.ExitReason)
}
