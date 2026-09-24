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
		`INSERT INTO gateways (id, account_id, label, noise_pubkey, services)
		 VALUES ($1, $2, $3, $4, $5)`,
		g.ID, g.AccountID, g.Label, g.NoisePubkey, services)
	if err != nil {
		return fmt.Errorf("creating gateway: %w", err)
	}
	return nil
}

const gatewayColumns = `id, account_id, label, noise_pubkey, services, connected, created_at`

func scanGateway(row rowScanner) (Gateway, error) {
	var g Gateway
	var servicesRaw []byte
	err := row.Scan(&g.ID, &g.AccountID, &g.Label, &g.NoisePubkey, &servicesRaw, &g.Connected, &g.CreatedAt)
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
		`SELECT `+gatewayColumns+` FROM gateways WHERE account_id = $1 ORDER BY created_at`, accountID)
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
