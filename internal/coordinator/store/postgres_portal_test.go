//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPortalCredentialLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")

	require.NoError(t, s.CreatePortalCredential(ctx, PortalCredential{
		AccountID: "act_1", Username: "alice", PasswordHash: "hashed", Role: RoleCustomer,
	}))

	got, err := s.GetPortalCredentialByUsername(ctx, "alice")
	require.NoError(t, err)
	require.Equal(t, "act_1", got.AccountID)
	require.Equal(t, "hashed", got.PasswordHash)
	require.Equal(t, RoleCustomer, got.Role)

	_, err = s.GetPortalCredentialByUsername(ctx, "nobody")
	require.ErrorIs(t, err, ErrNotFound)

	// A second account can't take an already-registered username.
	mustAccount(t, s, "act_2")
	err = s.CreatePortalCredential(ctx, PortalCredential{
		AccountID: "act_2", Username: "alice", PasswordHash: "other", Role: RoleProvider,
	})
	require.ErrorIs(t, err, ErrDuplicate)
}

func TestSessionLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")

	idHash := []byte("a-fake-session-id-hash-32-bytes")
	require.NoError(t, s.CreateSession(ctx, Session{
		IDHash: idHash, AccountID: "act_1", Role: RoleProvider,
		ExpiresAt: time.Now().Add(time.Hour),
	}))

	got, err := s.GetSession(ctx, idHash)
	require.NoError(t, err)
	require.Equal(t, "act_1", got.AccountID)
	require.Equal(t, RoleProvider, got.Role)
	require.Nil(t, got.RevokedAt)

	require.NoError(t, s.RevokeSession(ctx, idHash))
	_, err = s.GetSession(ctx, idHash)
	require.ErrorIs(t, err, ErrNotFound, "a revoked session must not be returned as valid")

	// Revoking again, or a session that never existed, is a no-op not an error.
	require.NoError(t, s.RevokeSession(ctx, idHash))
	require.NoError(t, s.RevokeSession(ctx, []byte("never-existed")))
}

func TestSessionExpiry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")

	idHash := []byte("expired-session-id-hash-32-byte")
	require.NoError(t, s.CreateSession(ctx, Session{
		IDHash: idHash, AccountID: "act_1", Role: RoleCustomer,
		ExpiresAt: time.Now().Add(-time.Minute), // already expired
	}))

	_, err := s.GetSession(ctx, idHash)
	require.ErrorIs(t, err, ErrNotFound, "an expired session must not be returned as valid")
}

func TestListNodesByAccountScoping(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustAccount(t, s, "act_2")
	mustNode(t, s, "nod_1", "act_1")
	mustNode(t, s, "nod_2", "act_1")
	mustNode(t, s, "nod_3", "act_2")

	list, err := s.ListNodesByAccount(ctx, "act_1")
	require.NoError(t, err)
	require.Len(t, list, 2)
	for _, n := range list {
		require.Equal(t, "act_1", n.AccountID)
	}

	list, err = s.ListNodesByAccount(ctx, "act_2")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "nod_3", list[0].ID)

	list, err = s.ListNodesByAccount(ctx, "act_missing")
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestListTasksByAccountScoping(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	mustAccount(t, s, "act_1")
	mustAccount(t, s, "act_2")

	mkTask := func(id, accountID string) Task {
		return Task{
			ID: id, AccountID: accountID, State: TaskQueued,
			Image: "alpine@sha256:a", Limits: Limits{CPUCores: 1, MemoryMB: 256, DiskMB: 512, WallTimeoutS: 60},
			Requirements: Requirements{Arch: "amd64", Isolation: "podman"},
			Delivery:     AtMostOnce, Retry: Retry{MaxAttempts: 1},
		}
	}
	require.NoError(t, s.CreateTask(ctx, mkTask("tsk_1", "act_1")))
	require.NoError(t, s.CreateTask(ctx, mkTask("tsk_2", "act_1")))
	require.NoError(t, s.CreateTask(ctx, mkTask("tsk_3", "act_2")))

	list, err := s.ListTasksByAccount(ctx, "act_1", 100)
	require.NoError(t, err)
	require.Len(t, list, 2)
	for _, task := range list {
		require.Equal(t, "act_1", task.AccountID)
	}

	list, err = s.ListTasksByAccount(ctx, "act_1", 1)
	require.NoError(t, err)
	require.Len(t, list, 1, "limit must be respected")

	list, err = s.ListTasksByAccount(ctx, "act_missing", 100)
	require.NoError(t, err)
	require.Empty(t, list)
}
