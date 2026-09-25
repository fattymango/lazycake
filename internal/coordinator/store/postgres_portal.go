package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) CreatePortalCredential(ctx context.Context, c PortalCredential) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO portal_credentials (account_id, username, password_hash, role)
		 VALUES ($1, $2, $3, $4)`,
		c.AccountID, c.Username, c.PasswordHash, string(c.Role))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("creating portal credential: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetPortalCredentialByUsername(ctx context.Context, username string) (PortalCredential, error) {
	var c PortalCredential
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT account_id, username, password_hash, role, created_at
		 FROM portal_credentials WHERE username = $1`, username,
	).Scan(&c.AccountID, &c.Username, &c.PasswordHash, &role, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PortalCredential{}, ErrNotFound
	}
	if err != nil {
		return PortalCredential{}, fmt.Errorf("getting portal credential: %w", err)
	}
	c.Role = PortalRole(role)
	return c, nil
}

func (s *PostgresStore) CreateSession(ctx context.Context, sess Session) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (id_hash, account_id, role, expires_at)
		 VALUES ($1, $2, $3, $4)`,
		sess.IDHash, sess.AccountID, string(sess.Role), sess.ExpiresAt)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("creating session: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetSession(ctx context.Context, idHash []byte) (Session, error) {
	var sess Session
	var role string
	err := s.pool.QueryRow(ctx,
		`SELECT id_hash, account_id, role, created_at, expires_at, revoked_at
		 FROM sessions
		 WHERE id_hash = $1 AND revoked_at IS NULL AND expires_at > now()`, idHash,
	).Scan(&sess.IDHash, &sess.AccountID, &role, &sess.CreatedAt, &sess.ExpiresAt, &sess.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("getting session: %w", err)
	}
	sess.Role = PortalRole(role)
	return sess, nil
}

func (s *PostgresStore) RevokeSession(ctx context.Context, idHash []byte) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id_hash = $1 AND revoked_at IS NULL`, idHash)
	if err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}
	return nil
}
