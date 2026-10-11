package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) CreateGateway(ctx context.Context, g Gateway) error {
	services, err := json.Marshal(g.Services)
	if err != nil {
		return fmt.Errorf("marshalling services: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO gateways (id, account_id, label, noise_pubkey, services, token_hash)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		g.ID, g.AccountID, g.Label, g.NoisePubkey, services, g.TokenHash)
	if err != nil {
		return fmt.Errorf("creating gateway: %w", err)
	}
	return nil
}

const gatewayColumns = `id, account_id, label, noise_pubkey, services, connected, created_at, version, max_mbps, archived_at, token_hash`

func scanGateway(row rowScanner) (Gateway, error) {
	var g Gateway
	var servicesRaw []byte
	err := row.Scan(&g.ID, &g.AccountID, &g.Label, &g.NoisePubkey, &servicesRaw, &g.Connected, &g.CreatedAt, &g.Version, &g.MaxMbps, &g.ArchivedAt, &g.TokenHash)
	if err != nil {
		return Gateway{}, err
	}
	if len(servicesRaw) > 0 {
		if err := json.Unmarshal(servicesRaw, &g.Services); err != nil {
			return Gateway{}, fmt.Errorf("unmarshalling services: %w", err)
		}
	}
	return g, nil
}

func (s *PostgresStore) GetGateway(ctx context.Context, id string) (Gateway, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+gatewayColumns+` FROM gateways WHERE id = $1`, id)
	g, err := scanGateway(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Gateway{}, ErrNotFound
	}
	if err != nil {
		return Gateway{}, fmt.Errorf("getting gateway: %w", err)
	}
	return g, nil
}

func (s *PostgresStore) ListGatewaysByAccount(ctx context.Context, accountID string) ([]Gateway, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+gatewayColumns+` FROM gateways WHERE account_id = $1 AND archived_at IS NULL ORDER BY created_at`, accountID)
	if err != nil {
		return nil, fmt.Errorf("listing gateways: %w", err)
	}
	defer rows.Close()

	var out []Gateway
	for rows.Next() {
		g, err := scanGateway(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning gateway: %w", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *PostgresStore) SetGatewayConnected(ctx context.Context, id string, connected bool, noisePubkey []byte) error {
	var err error
	if connected && len(noisePubkey) > 0 {
		_, err = s.pool.Exec(ctx,
			`UPDATE gateways SET connected = $2, noise_pubkey = $3 WHERE id = $1`,
			id, connected, noisePubkey)
	} else {
		_, err = s.pool.Exec(ctx, `UPDATE gateways SET connected = $2 WHERE id = $1`, id, connected)
	}
	if err != nil {
		return fmt.Errorf("setting gateway connected: %w", err)
	}
	return nil
}

func (s *PostgresStore) ResetGatewaysConnected(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `UPDATE gateways SET connected = false WHERE connected`); err != nil {
		return fmt.Errorf("resetting gateways connected: %w", err)
	}
	return nil
}

func (s *PostgresStore) SetGatewayVersion(ctx context.Context, id, version string) error {
	_, err := s.pool.Exec(ctx, `UPDATE gateways SET version = $2 WHERE id = $1 AND version <> $2`, id, version)
	if err != nil {
		return fmt.Errorf("setting gateway version: %w", err)
	}
	return nil
}

func (s *PostgresStore) ArchiveGateway(ctx context.Context, accountID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("archiving gateway: %w", err)
	}
	defer tx.Rollback(ctx)

	var tokenHash []byte
	err = tx.QueryRow(ctx,
		`SELECT token_hash FROM gateways WHERE id = $1 AND account_id = $2 AND archived_at IS NULL FOR UPDATE`,
		id, accountID).Scan(&tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("locking gateway: %w", err)
	}

	var inUse bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM tasks WHERE $1 = ANY(gateway_ids)
		   AND state IN ('queued', 'reserved', 'dispatched', 'running'))`, id).Scan(&inUse); err != nil {
		return fmt.Errorf("checking gateway use: %w", err)
	}
	if inUse {
		return ErrGatewayInUse
	}

	if _, err := tx.Exec(ctx, `UPDATE gateways SET archived_at = now(), connected = false WHERE id = $1`, id); err != nil {
		return fmt.Errorf("archiving gateway: %w", err)
	}
	if len(tokenHash) > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE api_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash); err != nil {
			return fmt.Errorf("revoking gateway token: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) SetGatewayMaxMbps(ctx context.Context, id string, mbps int) error {
	_, err := s.pool.Exec(ctx, `UPDATE gateways SET max_mbps = $2 WHERE id = $1 AND max_mbps IS DISTINCT FROM $2`, id, mbps)
	if err != nil {
		return fmt.Errorf("setting gateway bandwidth limit: %w", err)
	}
	return nil
}
