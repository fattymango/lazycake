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

	"golang.org/x/crypto/bcrypt"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/id"
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

// portalDemoBalanceMicros is what the seeded customer account starts
// with - enough to submit a comfortable number of small demo tasks
// (matches EnsureDemoAccount's own act_demo funding) without ever
// needing a manual "add funds" click just to try the portal out.
const portalDemoBalanceMicros = 1_000_000_000

// EnsurePortalDemoAccounts creates two demo portal login accounts
// (idempotently, by username): a customer account, pre-funded so
// submitting a task works immediately, and a provider account,
// deliberately unfunded (a provider earns rather than spends - see
// docs/01-dashboard-portals/OPEN_QUESTIONS.md). Both get the same
// password - fine for a demo whose whole point is trying both roles
// yourself, not a real multi-tenant security boundary. Same posture as
// EnsureDemoAccount: gated behind an explicit opt-in
// (cmd/coordinator/run.go only calls this when LAZYCAKE_SEED_PORTAL_*
// env vars are set), never run unless asked, safe to call on every
// startup since it's a no-op once the usernames already exist - so a
// `podman compose down -v && up` reseeds them exactly like act_demo.
func EnsurePortalDemoAccounts(ctx context.Context, st store.Store, customerUsername, providerUsername, password string) error {
	if customerUsername == "" || providerUsername == "" || password == "" {
		return fmt.Errorf("seed: customerUsername, providerUsername and password must all be set")
	}
	if err := ensurePortalAccount(ctx, st, customerUsername, password, store.RoleCustomer, portalDemoBalanceMicros); err != nil {
		return fmt.Errorf("seeding portal customer account %q: %w", customerUsername, err)
	}
	if err := ensurePortalAccount(ctx, st, providerUsername, password, store.RoleProvider, 0); err != nil {
		return fmt.Errorf("seeding portal provider account %q: %w", providerUsername, err)
	}
	return nil
}

func ensurePortalAccount(ctx context.Context, st store.Store, username, password string, role store.PortalRole, balanceMicros int64) error {
	if _, err := st.GetPortalCredentialByUsername(ctx, username); err == nil {
		return nil // already provisioned
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("checking for existing portal account: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	accountID := id.New(id.Account)
	if err := st.CreateAccount(ctx, store.Account{ID: accountID, Name: username, BalanceMicros: balanceMicros}); err != nil {
		return fmt.Errorf("creating account: %w", err)
	}
	if err := st.CreatePortalCredential(ctx, store.PortalCredential{
		AccountID: accountID, Username: username, PasswordHash: string(hash), Role: role,
	}); err != nil {
		return fmt.Errorf("creating portal credential: %w", err)
	}
	return nil
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
