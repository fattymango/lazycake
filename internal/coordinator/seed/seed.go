// Package seed provisions a fixed demo account and tokens for local/demo
// use, since there is no signup flow (PLAN.md non-goals: "no OAuth,
// password reset, email - bearer tokens in a table"). Something has to
// create the first token; this is that something, gated behind an
// explicit opt-in env var, never run unless asked.
package seed

import (
	"context"
	"errors"
	"fmt"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

const demoAccountID = "act_demo"

// EnsureDemoAccount creates act_demo (idempotently) plus a customer token
// equal to customerToken and an agent token equal to customerToken+"-agent"
// if they don't already exist.
func EnsureDemoAccount(ctx context.Context, st store.Store, customerToken string) error {
	if customerToken == "" {
		return fmt.Errorf("seed: customerToken must not be empty")
	}

	if _, err := st.GetAccount(ctx, demoAccountID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("checking for demo account: %w", err)
		}
		if err := st.CreateAccount(ctx, store.Account{ID: demoAccountID, Name: "demo", BalanceMicros: 1_000_000_000}); err != nil {
			return fmt.Errorf("creating demo account: %w", err)
		}
	}

	if err := ensureToken(ctx, st, customerToken, store.TokenCustomer); err != nil {
		return err
	}
	return ensureToken(ctx, st, customerToken+"-agent", store.TokenAgent)
}

const (
	platformAccountID = "act_platform_canary"
	platformGatewayID = "gw_platform_canary"
)

// EnsurePlatformCanaryAccount creates the platform's own account and
// gateway row (idempotently) that task 5.2's canary tasks dispatch
// against and bill to. It does not start a real gateway process - an
// operator still has to run one with this exact gateway ID against
// whatever local service the canary workload expects (see
// OPEN_QUESTIONS.md); until then, canary injection fails closed (no
// published Noise key means dispatchMessage refuses to build a Dispatch
// for it, logged and skipped - never sent broken).
func EnsurePlatformCanaryAccount(ctx context.Context, st store.Store) (accountID, gatewayID string, err error) {
	if _, err := st.GetAccount(ctx, platformAccountID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return "", "", fmt.Errorf("checking for platform canary account: %w", err)
		}
		// Funded generously: this account only ever pays itself (the
		// platform is both the "customer" and, via whichever node runs
		// the canary, part of the credit side too), so its balance isn't
		// really being spent down in any meaningful sense - it just needs
		// to never trip task 4.5's affordability check.
		if err := st.CreateAccount(ctx, store.Account{ID: platformAccountID, Name: "platform-canary", BalanceMicros: 1_000_000_000_000}); err != nil {
			return "", "", fmt.Errorf("creating platform canary account: %w", err)
		}
	}

	if _, err := st.GetGateway(ctx, platformGatewayID); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return "", "", fmt.Errorf("checking for platform canary gateway: %w", err)
		}
		if err := st.CreateGateway(ctx, store.Gateway{ID: platformGatewayID, AccountID: platformAccountID, Label: "platform-canary"}); err != nil {
			return "", "", fmt.Errorf("creating platform canary gateway: %w", err)
		}
	}

	return platformAccountID, platformGatewayID, nil
}

func ensureToken(ctx context.Context, st store.Store, token string, kind store.TokenKind) error {
	hash := auth.Hash(token)
	if _, err := st.Authenticate(ctx, hash); err == nil {
		return nil // already provisioned
	}
	if err := st.CreateToken(ctx, store.APIToken{TokenHash: hash, AccountID: demoAccountID, Kind: kind}); err != nil {
		return fmt.Errorf("creating %s token: %w", kind, err)
	}
	return nil
}
