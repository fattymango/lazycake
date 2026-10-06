package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/pricing"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/id"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// maxGateways is the "a task reaches at most three registered gateways and
// nothing else" rule from PLAN.md's overview, enforced at submission.
const maxGateways = 3

// CustomerServer implements lazycakev1.CustomerServiceServer: everything
// lcctl talks to.
type CustomerServer struct {
	lazycakev1.UnimplementedCustomerServiceServer

	Store store.Store
	// Rates prices task 4.5's submission-time affordability check. Zero
	// value (the Go zero Rates{}) means every rate is 0, which would let
	// everything through - callers should set this to pricing.
	// DefaultRates() (or their own), matching whatever cmd/coordinator
	// wires into billing.Ledger, so the up-front check and the actual
	// eventual charge agree.
	Rates pricing.Rates
	// Bus, if set, publishes a "queued" task_state event on submission for
	// task 6.1's SSE stream. A nil Bus is a valid no-op.
	Bus *events.Bus
	// Registry lets CancelTaskForAccount tell a task's node to stop it right
	// away. Optional: without it the scheduler's periodic resend still gets
	// the message there, just a few seconds later.
	Registry *Registry
}

var _ lazycakev1.CustomerServiceServer = (*CustomerServer)(nil)

func (s *CustomerServer) rates() pricing.Rates {
	if s.Rates == (pricing.Rates{}) {
		return pricing.DefaultRates()
	}
	return s.Rates
}

func (s *CustomerServer) authenticate(ctx context.Context) (store.APIToken, error) {
	token, ok := bearerToken(ctx)
	if !ok {
		return store.APIToken{}, status.Error(codes.Unauthenticated, "missing bearer token")
	}
	tok, err := s.Store.Authenticate(ctx, auth.Hash(token))
	if err != nil {
		return store.APIToken{}, status.Error(codes.Unauthenticated, "invalid token")
	}
	if tok.Kind != store.TokenCustomer {
		return store.APIToken{}, status.Error(codes.PermissionDenied, "token is not a customer token")
	}
	return tok, nil
}

func (s *CustomerServer) SubmitTask(ctx context.Context, req *lazycakev1.SubmitTaskRequest) (*lazycakev1.SubmitTaskResponse, error) {
	tok, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}

	limits := req.GetLimits()
	if limits == nil || limits.GetWallTimeoutS() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "limits.wall_timeout_s is required and must be > 0")
	}
	targets := make([]TunnelTargetSpec, len(req.GetTargets()))
	for i, spec := range req.GetTargets() {
		targets[i] = TunnelTargetSpec{GatewayID: spec.GetGatewayId(), Hostname: spec.GetHostname(), Port: spec.GetPort()}
	}

	task, err := s.SubmitTaskForAccount(ctx, tok.AccountID, SubmitTaskParams{
		Image: req.GetImage(), Entrypoint: req.GetEntrypoint(), Args: req.GetArgs(),
		Env: req.GetEnv(), Workdir: req.GetWorkdir(),
		Limits: store.Limits{
			CPUCores: limits.GetCpuCores(), MemoryMB: int(limits.GetMemoryMb()), DiskMB: int(limits.GetDiskMb()),
			TmpfsMB: int(limits.GetTmpfsMb()), PIDs: int(limits.GetPids()),
			WallTimeoutS: int(limits.GetWallTimeoutS()), NoOutputTimeoutS: int(limits.GetNoOutputTimeoutS()),
			EgressMB: int(limits.GetEgressMb()),
		},
		Targets: targets, Delivery: req.GetDelivery(), IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, err
	}
	return &lazycakev1.SubmitTaskResponse{TaskId: task.ID}, nil
}

// TunnelTargetSpec is SubmitTaskParams' transport-agnostic form of a tunnel
// target, mirroring lazycakev1.TunnelTargetSpec.
type TunnelTargetSpec struct {
	GatewayID string
	Hostname  string
	Port      int32
}

// SubmitTaskParams is the account-scoped, transport-agnostic form of a
// task submission - shared by the gRPC SubmitTask RPC above and
// portalapi's POST /api/portal/customer/tasks (IMPLEMENTATION.md task
// 7.4), so the digest-pin check, gateway-ownership check, and balance
// affordability check exist in exactly one place regardless of which
// transport a submission arrives over.
type SubmitTaskParams struct {
	Image          string
	Entrypoint     []string
	Args           []string
	Env            map[string]string
	Workdir        string
	Limits         store.Limits
	Targets        []TunnelTargetSpec
	Delivery       string
	IdempotencyKey string
}

// SubmitTaskForAccount validates and creates a task for accountID, exactly
// as the gRPC SubmitTask RPC does for whichever account its bearer token
// resolves to. Returned errors are *status.Error (codes.InvalidArgument,
// codes.FailedPrecondition, codes.AlreadyExists, codes.Internal) even
// though this method itself is transport-agnostic, so callers on any
// transport - gRPC directly, or portalapi mapping codes to HTTP statuses -
// get the same, single source of truth for what a given failure means.
func (s *CustomerServer) SubmitTaskForAccount(ctx context.Context, accountID string, p SubmitTaskParams) (store.Task, error) {
	if !strings.Contains(p.Image, "@sha256:") {
		return store.Task{}, status.Error(codes.InvalidArgument, "image must be digest-pinned, e.g. repo@sha256:...")
	}
	if len(p.Targets) > maxGateways {
		return store.Task{}, status.Errorf(codes.InvalidArgument, "at most %d gateways per task, got %d", maxGateways, len(p.Targets))
	}
	targets := make([]store.TunnelTarget, len(p.Targets))
	gatewayIDs := make([]string, len(p.Targets))
	for i, spec := range p.Targets {
		if spec.Hostname == "" || spec.Port <= 0 {
			return store.Task{}, status.Errorf(codes.InvalidArgument, "target %d: hostname and port are required", i)
		}
		gw, err := s.Store.GetGateway(ctx, spec.GatewayID)
		if err != nil {
			if err == store.ErrNotFound {
				return store.Task{}, status.Errorf(codes.InvalidArgument, "gateway %s not found", spec.GatewayID)
			}
			return store.Task{}, status.Errorf(codes.Internal, "getting gateway %s: %v", spec.GatewayID, err)
		}
		if gw.AccountID != accountID {
			// Same error as not-found: don't reveal that a gateway ID
			// belongs to someone else.
			return store.Task{}, status.Errorf(codes.InvalidArgument, "gateway %s not found", spec.GatewayID)
		}
		targets[i] = store.TunnelTarget{GatewayID: spec.GatewayID, Hostname: spec.Hostname, Port: spec.Port}
		gatewayIDs[i] = spec.GatewayID
	}
	if p.Limits.WallTimeoutS <= 0 {
		return store.Task{}, status.Error(codes.InvalidArgument, "limits.wall_timeout_s is required and must be > 0")
	}

	delivery := store.AtMostOnce
	if p.Delivery == string(store.AtLeastOnce) {
		delivery = store.AtLeastOnce
	}

	taskID := id.New(id.Task)
	var idemKey *string
	if p.IdempotencyKey != "" {
		k := p.IdempotencyKey
		idemKey = &k
	}

	task := store.Task{
		ID: taskID, AccountID: accountID, State: store.TaskQueued,
		IdempotencyKey: idemKey,
		Image:          p.Image,
		Entrypoint:     p.Entrypoint,
		Args:           p.Args,
		Env:            p.Env,
		Workdir:        p.Workdir,
		Limits:         p.Limits,
		Requirements:   store.Requirements{Arch: "amd64", Isolation: "podman", Confidentiality: "none"},
		GatewayIDs:     gatewayIDs,
		TunnelTargets:  targets,
		Delivery:       delivery,
		Retry:          store.Retry{MaxAttempts: 1},
	}
	if delivery == store.AtLeastOnce {
		task.Retry.MaxAttempts = 3
	}
	if task.Limits.PIDs == 0 {
		task.Limits.PIDs = 256
	}

	// Balance enforcement (task 4.5): reject if the account can't cover
	// the worst case (wall_timeout_s at the full provisioned rate) -
	// checked against available balance (balance minus active holds), not
	// raw balance, so this remains a real limit across many concurrently
	// in-flight tasks, not just a one-time gate at submission.
	worstCase := pricing.WorstCase(s.rates(), task.Limits)
	available, err := s.Store.AvailableBalance(ctx, accountID)
	if err != nil {
		return store.Task{}, status.Errorf(codes.Internal, "checking account balance: %v", err)
	}
	if available < worstCase {
		return store.Task{}, status.Errorf(codes.FailedPrecondition,
			"insufficient balance: available %d micros, worst case for this task is %d micros", available, worstCase)
	}

	if err := s.Store.CreateTask(ctx, task); err != nil {
		if err == store.ErrDuplicate {
			return store.Task{}, status.Error(codes.AlreadyExists, "idempotency_key already used")
		}
		return store.Task{}, status.Errorf(codes.Internal, "creating task: %v", err)
	}
	s.Bus.Publish(events.Event{Type: "task_state", AtMS: time.Now().UnixMilli(), AccountID: accountID, TaskID: taskID, State: string(store.TaskQueued)})

	task.CreatedAt = time.Now()
	return task, nil
}

func (s *CustomerServer) GetTask(ctx context.Context, req *lazycakev1.GetTaskRequest) (*lazycakev1.TaskStatus, error) {
	tok, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	task, err := s.Store.GetTask(ctx, req.GetTaskId())
	if err != nil {
		if err == store.ErrNotFound {
			return nil, status.Error(codes.NotFound, "task not found")
		}
		return nil, status.Errorf(codes.Internal, "getting task: %v", err)
	}
	if task.AccountID != tok.AccountID {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	return taskStatusProto(task), nil
}

func taskStatusProto(t store.Task) *lazycakev1.TaskStatus {
	out := &lazycakev1.TaskStatus{
		TaskId: t.ID, State: string(t.State), CreatedAtUnixMs: t.CreatedAt.UnixMilli(),
	}
	if t.NodeID != nil {
		out.NodeId = *t.NodeID
	}
	if t.ExitCode != nil {
		out.ExitCode = int32(*t.ExitCode)
		out.HasExitCode = true
	}
	if t.ExitReason != nil {
		out.ExitReason = *t.ExitReason
	}
	if t.StartedAt != nil {
		out.StartedAtUnixMs = t.StartedAt.UnixMilli()
	}
	if t.FinishedAt != nil {
		out.FinishedAtUnixMs = t.FinishedAt.UnixMilli()
	}
	out.CancelRequested = t.CancelRequestedAt != nil && !isFinishedState(t.State)
	return out
}

func (s *CustomerServer) StreamLogs(req *lazycakev1.StreamLogsRequest, stream lazycakev1.CustomerService_StreamLogsServer) error {
	ctx := stream.Context()
	tok, err := s.authenticate(ctx)
	if err != nil {
		return err
	}
	task, err := s.Store.GetTask(ctx, req.GetTaskId())
	if err != nil {
		if err == store.ErrNotFound {
			return status.Error(codes.NotFound, "task not found")
		}
		return status.Errorf(codes.Internal, "getting task: %v", err)
	}
	if task.AccountID != tok.AccountID {
		return status.Error(codes.NotFound, "task not found")
	}

	var sinceSeq int64
	terminal := func(st store.TaskState) bool {
		switch st {
		case store.TaskSucceeded, store.TaskFailed, store.TaskFenced, store.TaskAbandoned, store.TaskCancelled:
			return true
		}
		return false
	}

	for {
		lines, err := s.Store.ListLogs(ctx, req.GetTaskId(), sinceSeq)
		if err != nil {
			return status.Errorf(codes.Internal, "listing logs: %v", err)
		}
		for _, l := range lines {
			if err := stream.Send(&lazycakev1.TaskLogLine{
				Seq: l.Seq, Stream: l.Stream, AtUnixMs: l.At.UnixMilli(), Line: l.Line,
			}); err != nil {
				return err
			}
			sinceSeq = l.Seq
		}

		if !req.GetFollow() {
			return nil
		}

		task, err := s.Store.GetTask(ctx, req.GetTaskId())
		if err != nil {
			return status.Errorf(codes.Internal, "getting task: %v", err)
		}
		if terminal(task.State) && len(lines) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *CustomerServer) CreateGateway(ctx context.Context, req *lazycakev1.CreateGatewayRequest) (*lazycakev1.CreateGatewayResponse, error) {
	tok, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	if req.GetLabel() == "" {
		return nil, status.Error(codes.InvalidArgument, "label is required")
	}

	services, err := parseGatewayServices(req.GetServices())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	gatewayID := id.New(id.Gateway)
	installToken, err := randomToken()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generating install token: %v", err)
	}

	if err := s.Store.CreateGateway(ctx, store.Gateway{
		ID: gatewayID, AccountID: tok.AccountID, Label: req.GetLabel(), Services: services,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "creating gateway: %v", err)
	}
	if err := s.Store.CreateToken(ctx, store.APIToken{
		TokenHash: auth.Hash(installToken), AccountID: tok.AccountID, Kind: store.TokenGateway,
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "creating gateway token: %v", err)
	}

	return &lazycakev1.CreateGatewayResponse{GatewayId: gatewayID, InstallToken: installToken}, nil
}

func parseGatewayServices(specs []string) ([]store.GatewayService, error) {
	out := make([]store.GatewayService, 0, len(specs))
	for _, spec := range specs {
		name, portStr, ok := strings.Cut(spec, ":")
		if !ok {
			return nil, fmt.Errorf("malformed service %q, want name:port", spec)
		}
		var port int
		if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port <= 0 {
			return nil, fmt.Errorf("malformed port in service %q", spec)
		}
		out = append(out, store.GatewayService{Name: name, Port: port})
	}
	return out, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *CustomerServer) ListGateways(ctx context.Context, _ *lazycakev1.ListGatewaysRequest) (*lazycakev1.ListGatewaysResponse, error) {
	tok, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	gateways, err := s.Store.ListGatewaysByAccount(ctx, tok.AccountID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing gateways: %v", err)
	}
	out := &lazycakev1.ListGatewaysResponse{}
	for _, g := range gateways {
		svcNames := make([]string, len(g.Services))
		for i, svc := range g.Services {
			svcNames[i] = fmt.Sprintf("%s:%d", svc.Name, svc.Port)
		}
		out.Gateways = append(out.Gateways, &lazycakev1.GatewayStatus{
			GatewayId: g.ID, Label: g.Label, Connected: g.Connected, Services: svcNames,
		})
	}
	return out, nil
}

func (s *CustomerServer) ListNodes(ctx context.Context, _ *lazycakev1.ListNodesRequest) (*lazycakev1.ListNodesResponse, error) {
	if _, err := s.authenticate(ctx); err != nil {
		return nil, err
	}
	nodes, err := s.Store.ListNodes(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing nodes: %v", err)
	}
	out := &lazycakev1.ListNodesResponse{}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, &lazycakev1.NodeStatus{
			NodeId: n.ID, Hostname: n.Hostname, Connected: n.Connected,
			OfferCores: n.OfferCores, OfferMemoryMb: int32(n.OfferMemoryMB), OfferDiskMb: int32(n.OfferDiskMB),
			TrustScore: n.TrustScore,
		})
	}
	return out, nil
}
