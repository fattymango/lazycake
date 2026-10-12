package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) CreateOAuthClient(ctx context.Context, c OAuthClient) error {
	uris, err := json.Marshal(c.RedirectURIs)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO oauth_clients (client_id, client_name, redirect_uris) VALUES ($1, $2, $3)`, c.ClientID, c.ClientName, uris); err != nil {
		return fmt.Errorf("creating oauth client: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetOAuthClient(ctx context.Context, clientID string) (OAuthClient, error) {
	var c OAuthClient
	var uris []byte
	err := s.pool.QueryRow(ctx, `SELECT client_id, client_name, redirect_uris, created_at FROM oauth_clients WHERE client_id = $1`, clientID).
		Scan(&c.ClientID, &c.ClientName, &uris, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthClient{}, ErrNotFound
	}
	if err != nil {
		return OAuthClient{}, fmt.Errorf("getting oauth client: %w", err)
	}
	if err := json.Unmarshal(uris, &c.RedirectURIs); err != nil {
		return OAuthClient{}, fmt.Errorf("reading redirect uris: %w", err)
	}
	return c, nil
}

func (s *PostgresStore) CreateOAuthCode(ctx context.Context, c OAuthCode) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_codes (code_hash, client_id, account_id, role, redirect_uri, code_challenge, expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		c.CodeHash, c.ClientID, c.AccountID, string(c.Role), c.RedirectURI, c.CodeChallenge, c.ExpiresAt); err != nil {
		return fmt.Errorf("creating oauth code: %w", err)
	}
	return nil
}

func (s *PostgresStore) ConsumeOAuthCode(ctx context.Context, codeHash []byte) (OAuthCode, error) {
	var c OAuthCode
	var role string
	err := s.pool.QueryRow(ctx,
		`DELETE FROM oauth_codes WHERE code_hash = $1 AND expires_at > now()
		 RETURNING code_hash, client_id, account_id, role, redirect_uri, code_challenge, expires_at`, codeHash).
		Scan(&c.CodeHash, &c.ClientID, &c.AccountID, &role, &c.RedirectURI, &c.CodeChallenge, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthCode{}, ErrNotFound
	}
	if err != nil {
		return OAuthCode{}, fmt.Errorf("consuming oauth code: %w", err)
	}
	c.Role = PortalRole(role)
	return c, nil
}

func (s *PostgresStore) CreateOAuthToken(ctx context.Context, t OAuthToken) error {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_tokens (id, access_hash, refresh_hash, client_id, account_id, role, access_expires_at, refresh_expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.ID, t.AccessHash, t.RefreshHash, t.ClientID, t.AccountID, string(t.Role), t.AccessExpiresAt, t.RefreshExpiresAt); err != nil {
		return fmt.Errorf("creating oauth token: %w", err)
	}
	return nil
}

func scanOAuthToken(row pgx.Row) (OAuthToken, error) {
	var t OAuthToken
	var role string
	err := row.Scan(&t.ID, &t.AccessHash, &t.RefreshHash, &t.ClientID, &t.AccountID, &role, &t.AccessExpiresAt, &t.RefreshExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthToken{}, ErrNotFound
	}
	if err != nil {
		return OAuthToken{}, fmt.Errorf("reading oauth token: %w", err)
	}
	t.Role = PortalRole(role)
	return t, nil
}

const oauthTokenColumns = `id, access_hash, refresh_hash, client_id, account_id, role, access_expires_at, refresh_expires_at`

func (s *PostgresStore) GetOAuthTokenByAccess(ctx context.Context, accessHash []byte) (OAuthToken, error) {
	return scanOAuthToken(s.pool.QueryRow(ctx,
		`UPDATE oauth_tokens SET last_used_at = now() WHERE access_hash = $1 AND revoked_at IS NULL AND access_expires_at > now() RETURNING `+oauthTokenColumns,
		accessHash))
}

func (s *PostgresStore) RotateOAuthToken(ctx context.Context, refreshHash []byte, next OAuthToken) (OAuthToken, error) {
	return scanOAuthToken(s.pool.QueryRow(ctx,
		`UPDATE oauth_tokens SET access_hash = $2, refresh_hash = $3, access_expires_at = $4, refresh_expires_at = $5, last_used_at = now()
		 WHERE refresh_hash = $1 AND client_id = $6 AND revoked_at IS NULL AND refresh_expires_at > now() RETURNING `+oauthTokenColumns,
		refreshHash, next.AccessHash, next.RefreshHash, next.AccessExpiresAt, next.RefreshExpiresAt, next.ClientID))
}

func (s *PostgresStore) ListOAuthGrants(ctx context.Context, accountID string) ([]OAuthGrant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT t.id, c.client_name, t.created_at, t.last_used_at FROM oauth_tokens t JOIN oauth_clients c USING (client_id)
		 WHERE t.account_id = $1 AND t.revoked_at IS NULL AND t.refresh_expires_at > now() ORDER BY t.created_at DESC`, accountID)
	if err != nil {
		return nil, fmt.Errorf("listing oauth grants: %w", err)
	}
	defer rows.Close()
	var out []OAuthGrant
	for rows.Next() {
		var g OAuthGrant
		if err := rows.Scan(&g.ID, &g.ClientName, &g.CreatedAt, &g.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *PostgresStore) RevokeOAuthGrant(ctx context.Context, accountID, grantID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE oauth_tokens SET revoked_at = now() WHERE id = $1 AND account_id = $2 AND revoked_at IS NULL`, grantID, accountID)
	if err != nil {
		return fmt.Errorf("revoking oauth grant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
