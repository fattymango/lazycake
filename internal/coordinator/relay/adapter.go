// Package relay adapts the coordinator's store.Store to the two small
// interfaces internal/tunnel/quic.Relay depends on (Authenticator,
// GatewayRegistry), so the transport package never imports coordinator
// code and the coordinator's wiring for "what a token/gateway means" stays
// in one place.
package relay

import (
	"context"
	"fmt"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// StoreAdapter implements quic.Authenticator and quic.GatewayRegistry
// against a store.Store.
type StoreAdapter struct {
	Store store.Store
}

func (a StoreAdapter) Authenticate(ctx context.Context, token string) (string, error) {
	tok, err := a.Store.Authenticate(ctx, auth.Hash(token))
	if err != nil {
		return "", fmt.Errorf("authenticating: %w", err)
	}
	return tok.AccountID, nil
}

func (a StoreAdapter) OwnsGateway(ctx context.Context, accountID, gatewayID string) (bool, error) {
	gw, err := a.Store.GetGateway(ctx, gatewayID)
	if err != nil {
		if err == store.ErrNotFound {
			return false, nil
		}
		return false, fmt.Errorf("getting gateway: %w", err)
	}
	return gw.AccountID == accountID, nil
}

func (a StoreAdapter) SetGatewayConnected(ctx context.Context, gatewayID string, connected bool, noisePubkey []byte) error {
	return a.Store.SetGatewayConnected(ctx, gatewayID, connected, noisePubkey)
}
