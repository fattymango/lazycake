package api

import (
	"context"
	"strings"

	"google.golang.org/grpc/metadata"
)

// bearerToken extracts the token from an "authorization: Bearer <token>"
// gRPC metadata header, used by the customer-facing RPCs (the agent
// stream authenticates differently, via its first message).
func bearerToken(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(vals[0], prefix) {
		return "", false
	}
	return strings.TrimPrefix(vals[0], prefix), true
}
