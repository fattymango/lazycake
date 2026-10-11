package api

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/fattymango/lazycake/internal/coordinator/auth"
	"github.com/fattymango/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/fattymango/lazycake/internal/proto/lazycake/v1"
)

// GatewayEvents receives byte reports off GatewayServer, for task 4.3's
// byte reconciliation. *billing.Reconciler satisfies this structurally.
type GatewayEvents interface {
	RecordGatewayBytes(taskID string, bytes int64)
}

type noopGatewayEvents struct{}

func (noopGatewayEvents) RecordGatewayBytes(string, int64) {}

// GatewayServer implements lazycakev1.GatewayServiceServer: the one RPC a
// gateway process calls, to report the byte counts it observed forwarding
// one task's traffic (see internal/gateway/listener.Listener.OnForward).
type GatewayServer struct {
	lazycakev1.UnimplementedGatewayServiceServer

	Store  store.Store
	Events GatewayEvents // may be nil until billing is wired in
}

var _ lazycakev1.GatewayServiceServer = (*GatewayServer)(nil)

func (s *GatewayServer) events() GatewayEvents {
	if s.Events == nil {
		return noopGatewayEvents{}
	}
	return s.Events
}

func (s *GatewayServer) ReportBytes(ctx context.Context, req *lazycakev1.ByteReport) (*lazycakev1.ByteReportAck, error) {
	token, ok := bearerToken(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing bearer token")
	}
	tok, err := s.Store.Authenticate(ctx, auth.Hash(token))
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	if tok.Kind != store.TokenGateway {
		return nil, status.Error(codes.PermissionDenied, "token is not a gateway token")
	}

	gw, err := s.Store.GetGateway(ctx, req.GetGatewayId())
	if err != nil {
		return nil, status.Error(codes.NotFound, "unknown gateway")
	}
	if gw.AccountID != tok.AccountID {
		return nil, status.Error(codes.PermissionDenied, "gateway does not belong to this token's account")
	}
	if gw.ArchivedAt != nil {
		return nil, status.Error(codes.PermissionDenied, "gateway was deleted")
	}

	toLocal, toTask := req.GetBytesToLocal(), req.GetBytesToTask()
	if toLocal < 0 || toTask < 0 {
		return nil, status.Error(codes.InvalidArgument, "byte counts can't be negative")
	}

	// Billing reconciliation keeps summing: a connection's reports are deltas that
	// add up to what one report at close used to carry.
	s.events().RecordGatewayBytes(req.GetTaskId(), toLocal+toTask)

	// Keep what the gateway moved, per task and over time (task 8.14). The per-task
	// numbers are only recorded for a task that exists and belongs to the gateway's
	// own account, so a gateway can't attribute traffic to someone else's task. A
	// gateway built before these fields sends one report at close with no service:
	// that is the whole connection.
	legacy := req.GetService() == ""
	// A batched report says how many connections closed; an older gateway says final=true for one.
	conns := int(req.GetConnections())
	if conns < 0 {
		conns = 0
	}
	if conns == 0 && (req.GetFinal() || legacy) {
		conns = 1
	}
	recordTask := false
	if t, err := s.Store.GetTask(ctx, req.GetTaskId()); err == nil && t.AccountID == gw.AccountID {
		recordTask = true
	}
	if err := s.Store.RecordGatewayTraffic(ctx, store.GatewayTrafficReport{
		GatewayID: gw.ID, TaskID: req.GetTaskId(), Service: req.GetService(),
		BytesToLocal: toLocal, BytesToTask: toTask,
		Final: conns > 0, Connections: conns, At: time.Now(), RecordTask: recordTask,
	}); err != nil {
		// The report was accepted for billing; failing the RPC would only make the
		// gateway resend what was already counted there.
		slog.Warn("recording gateway traffic", "gateway_id", gw.ID, "task_id", req.GetTaskId(), "error", err)
	}
	return &lazycakev1.ByteReportAck{}, nil
}

// Hello records the release a gateway runs, sent whenever it (re)connects. Display only: it only ever tells the customer whether
// their gateway is behind. A gateway built before this simply never calls it, and its version reads as unknown.
func (s *GatewayServer) Hello(ctx context.Context, req *lazycakev1.GatewayHello) (*lazycakev1.GatewayHelloAck, error) {
	token, ok := bearerToken(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing bearer token")
	}
	tok, err := s.Store.Authenticate(ctx, auth.Hash(token))
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	if tok.Kind != store.TokenGateway {
		return nil, status.Error(codes.PermissionDenied, "token is not a gateway token")
	}
	gw, err := s.Store.GetGateway(ctx, req.GetGatewayId())
	if err != nil {
		return nil, status.Error(codes.NotFound, "unknown gateway")
	}
	if gw.AccountID != tok.AccountID {
		return nil, status.Error(codes.PermissionDenied, "gateway does not belong to this token's account")
	}
	if gw.ArchivedAt != nil {
		return nil, status.Error(codes.PermissionDenied, "gateway was deleted")
	}
	if err := s.Store.SetGatewayVersion(ctx, gw.ID, capVersion(req.GetVersion())); err != nil {
		slog.Warn("recording gateway version", "gateway_id", gw.ID, "error", err)
	}
	return &lazycakev1.GatewayHelloAck{}, nil
}

// maxVersionLen bounds a version string an agent or gateway reports about itself. A real one is a dozen bytes ("v0.4.0-12-gabc1234-dirty"); the
// rest of the field is only there for whoever sends something else.
const maxVersionLen = 64

// capVersion cuts a reported version to maxVersionLen bytes without leaving half a character at the end, which the database would refuse.
func capVersion(v string) string {
	if len(v) <= maxVersionLen {
		return v
	}
	return strings.ToValidUTF8(v[:maxVersionLen], "")
}
