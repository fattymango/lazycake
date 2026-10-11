package ratelimit

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func peerIP(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return ClientIP(p.Addr.String())
	}
	return ""
}

// check refuses a blocked address; after reports a call's outcome, counting an Unauthenticated answer as a failure.
func check(g *FailGuard, ctx context.Context) (string, error) {
	ip := peerIP(ctx)
	if ip != "" && g.Blocked(ip) {
		return ip, status.Error(codes.ResourceExhausted, "too many failed authentication attempts: try again later")
	}
	return ip, nil
}

func after(g *FailGuard, ip string, err error) {
	if ip != "" && err != nil && status.Code(err) == codes.Unauthenticated {
		g.Fail(ip)
	}
}

// UnaryGuard is a gRPC interceptor that blocks an address after repeated failed authentication.
func UnaryGuard(g *FailGuard) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		ip, err := check(g, ctx)
		if err != nil {
			return nil, err
		}
		resp, err := h(ctx, req)
		after(g, ip, err)
		return resp, err
	}
}

// StreamGuard is UnaryGuard for streaming calls (the agent's long-lived stream).
func StreamGuard(g *FailGuard) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		ip, err := check(g, ss.Context())
		if err != nil {
			return err
		}
		err = h(srv, ss)
		after(g, ip, err)
		return err
	}
}
