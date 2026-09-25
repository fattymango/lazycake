package api

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// TestBalanceEnforcement is task 4.5's own verify: an account with $1 of
// credit cannot submit a task that could cost $10 (a long enough
// wall_timeout_s at the full provisioned rate to exceed it), but the same
// account can submit a cheap, short one.
func TestBalanceEnforcement(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust")), store.APIToken{AccountID: "act_poor", Kind: store.TokenCustomer})
	if err := fs.CreateAccount(context.Background(), store.Account{ID: "act_poor", Name: "act_poor", BalanceMicros: 1_000_000}); err != nil { // $1
		t.Fatalf("funding account: %v", err)
	}

	client := startCustomerTestServer(t, fs)

	// A big, long task: high provisioned resources for a long wall-clock
	// timeout, engineered (against DefaultRates) to worst-case well past
	// $10.
	_, err := client.SubmitTask(authCtx("cust"), &lazycakev1.SubmitTaskRequest{
		Image:  "alpine@sha256:abc",
		Limits: &lazycakev1.TaskLimits{CpuCores: 8, MemoryMb: 16384, DiskMb: 50000, WallTimeoutS: 36000, EgressMb: 50000},
	})
	if err == nil {
		t.Fatal("expected submission to be rejected for insufficient balance")
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", err)
	}

	fs.mu.Lock()
	numTasks := len(fs.tasks)
	fs.mu.Unlock()
	if numTasks != 0 {
		t.Fatal("a rejected submission must not create a task row")
	}

	// The same account can still afford a small, short task.
	resp, err := client.SubmitTask(authCtx("cust"), &lazycakev1.SubmitTaskRequest{
		Image:  "alpine@sha256:abc",
		Limits: &lazycakev1.TaskLimits{CpuCores: 1, MemoryMb: 256, DiskMb: 512, WallTimeoutS: 30},
	})
	if err != nil {
		t.Fatalf("expected a cheap task to be affordable, got: %v", err)
	}
	if resp.GetTaskId() == "" {
		t.Fatal("expected a task id")
	}
}

// TestSubmissionRejectionDoesNotHold confirms a rejected submission places
// no hold - AvailableBalance is unchanged.
func TestSubmissionRejectionDoesNotHold(t *testing.T) {
	fs := newFakeStore()
	fs.addToken(string(auth.Hash("cust")), store.APIToken{AccountID: "act_poor", Kind: store.TokenCustomer})
	if err := fs.CreateAccount(context.Background(), store.Account{ID: "act_poor", Name: "act_poor", BalanceMicros: 1_000_000}); err != nil {
		t.Fatalf("funding account: %v", err)
	}
	client := startCustomerTestServer(t, fs)

	before, err := fs.AvailableBalance(context.Background(), "act_poor")
	if err != nil {
		t.Fatalf("AvailableBalance: %v", err)
	}

	_, err = client.SubmitTask(authCtx("cust"), &lazycakev1.SubmitTaskRequest{
		Image:  "alpine@sha256:abc",
		Limits: &lazycakev1.TaskLimits{CpuCores: 8, MemoryMb: 16384, DiskMb: 50000, WallTimeoutS: 36000, EgressMb: 50000},
	})
	if err == nil {
		t.Fatal("expected rejection")
	}

	after, err := fs.AvailableBalance(context.Background(), "act_poor")
	if err != nil {
		t.Fatalf("AvailableBalance: %v", err)
	}
	if before != after {
		t.Fatalf("available balance changed after a rejected submission: %d -> %d", before, after)
	}
}
