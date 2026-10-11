package ratelimit

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestUnaryGuardBlocksAnAddressAfterRepeatedUnauthenticated(t *testing.T) {
	g := NewFailGuard(3, 60e9, 300e9)
	ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 4000}})
	other := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("203.0.113.8"), Port: 4000}})
	calls := 0
	deny := func(context.Context, any) (any, error) {
		calls++
		return nil, status.Error(codes.Unauthenticated, "bad token")
	}
	ok := func(context.Context, any) (any, error) { calls++; return "fine", nil }
	ic := UnaryGuard(g)

	for i := 0; i < 3; i++ {
		if _, err := ic(ctx, nil, &grpc.UnaryServerInfo{}, deny); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	before := calls
	if _, err := ic(ctx, nil, &grpc.UnaryServerInfo{}, ok); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("a blocked address must be refused before the handler runs, got %v", err)
	}
	if calls != before {
		t.Fatal("the handler ran for a blocked address")
	}
	if _, err := ic(other, nil, &grpc.UnaryServerInfo{}, ok); err != nil {
		t.Fatalf("another address is unaffected: %v", err)
	}
	// Failures that aren't authentication (e.g. a bad request) never count.
	bad := func(context.Context, any) (any, error) { return nil, status.Error(codes.InvalidArgument, "x") }
	for i := 0; i < 10; i++ {
		ic(other, nil, &grpc.UnaryServerInfo{}, bad)
	}
	if _, err := ic(other, nil, &grpc.UnaryServerInfo{}, ok); err != nil {
		t.Fatalf("non-auth errors must not block: %v", err)
	}
}
