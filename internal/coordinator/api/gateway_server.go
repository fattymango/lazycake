package api

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
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

	s.events().RecordGatewayBytes(req.GetTaskId(), req.GetBytesToLocal()+req.GetBytesToTask())
	return &lazycakev1.ByteReportAck{}, nil
}
