package main

import (
	"context"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// newClient dials the coordinator named by LAZYCAKE_COORDINATOR_ADDR and
// returns a client plus a context already carrying LAZYCAKE_TOKEN as a
// bearer credential, so every command just does client.Foo(ctx, req).
func newClient() (lazycakev1.CustomerServiceClient, context.Context, func(), error) {
	addr := os.Getenv("LAZYCAKE_COORDINATOR_ADDR")
	if addr == "" {
		return nil, nil, nil, fmt.Errorf("LAZYCAKE_COORDINATOR_ADDR is required")
	}
	token := os.Getenv("LAZYCAKE_TOKEN")
	if token == "" {
		return nil, nil, nil, fmt.Errorf("LAZYCAKE_TOKEN is required")
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("dialing %s: %w", addr, err)
	}

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	return lazycakev1.NewCustomerServiceClient(conn), ctx, func() { conn.Close() }, nil
}
