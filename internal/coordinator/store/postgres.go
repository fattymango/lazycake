package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store against a Postgres database reachable
// through pgx. It holds no state beyond the pool, so it is safe to share
// across goroutines.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects to databaseURL and returns a ready Store.
func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

// Close releases the underlying connection pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
}

// Pool exposes the underlying pgx pool for callers that need raw SQL
// outside the Store interface - test setup/teardown, mainly. Production
// code should go through Store's methods instead.
func (s *PostgresStore) Pool() *pgxpool.Pool {
	return s.pool
}

var _ Store = (*PostgresStore)(nil)

// --- Accounts ---

func (s *PostgresStore) CreateAccount(ctx context.Context, a Account) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO accounts (id, name, balance_micros) VALUES ($1, $2, $3)`,
		a.ID, a.Name, a.BalanceMicros)
	if err != nil {
		return fmt.Errorf("creating account: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetAccount(ctx context.Context, id string) (Account, error) {
	var a Account
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, balance_micros, created_at FROM accounts WHERE id = $1`, id,
	).Scan(&a.ID, &a.Name, &a.BalanceMicros, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("getting account: %w", err)
	}
	return a, nil
}

func (s *PostgresStore) AdjustBalance(ctx context.Context, id string, deltaMicros int64) (int64, error) {
	var balance int64
	err := s.pool.QueryRow(ctx,
		`UPDATE accounts SET balance_micros = balance_micros + $2 WHERE id = $1
		 RETURNING balance_micros`, id, deltaMicros,
	).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("adjusting balance: %w", err)
	}
	return balance, nil
}

// --- Tokens ---

func (s *PostgresStore) CreateToken(ctx context.Context, t APIToken) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO api_tokens (token_hash, account_id, kind) VALUES ($1, $2, $3)`,
		t.TokenHash, t.AccountID, string(t.Kind))
	if err != nil {
		return fmt.Errorf("creating token: %w", err)
	}
	return nil
}

func (s *PostgresStore) Authenticate(ctx context.Context, tokenHash []byte) (APIToken, error) {
	var t APIToken
	var kind string
	err := s.pool.QueryRow(ctx,
		`SELECT token_hash, account_id, kind, created_at, revoked_at
		 FROM api_tokens WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash,
	).Scan(&t.TokenHash, &t.AccountID, &kind, &t.CreatedAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIToken{}, ErrNotFound
	}
	if err != nil {
		return APIToken{}, fmt.Errorf("authenticating token: %w", err)
	}
	t.Kind = TokenKind(kind)
	return t, nil
}

// --- Nodes ---

func (s *PostgresStore) UpsertNode(ctx context.Context, n Node) error {
	caps, err := json.Marshal(n.Capabilities)
	if err != nil {
		return fmt.Errorf("marshalling capabilities: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO nodes (id, account_id, instance_id, hostname, arch, cpu_flags, capabilities,
			offer_cores, offer_memory_mb, offer_disk_mb, connected)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, true)
		ON CONFLICT (id) DO UPDATE SET
			instance_id = EXCLUDED.instance_id,
			hostname = EXCLUDED.hostname,
			arch = EXCLUDED.arch,
			cpu_flags = EXCLUDED.cpu_flags,
			capabilities = EXCLUDED.capabilities,
			offer_cores = EXCLUDED.offer_cores,
			offer_memory_mb = EXCLUDED.offer_memory_mb,
			offer_disk_mb = EXCLUDED.offer_disk_mb,
			connected = true`,
		n.ID, n.AccountID, nullIfEmpty(n.InstanceID), n.Hostname, n.Arch, orEmpty(n.CPUFlags), caps,
		n.OfferCores, n.OfferMemoryMB, n.OfferDiskMB)
	if err != nil {
		return fmt.Errorf("upserting node: %w", err)
	}
	return nil
}

const nodeColumns = `id, account_id, instance_id, hostname, arch, cpu_flags, capabilities,
	offer_cores, offer_memory_mb, offer_disk_mb, bench_score, trust_score,
	connected, last_heartbeat_at, created_at`

func (s *PostgresStore) GetNode(ctx context.Context, id string) (Node, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE id = $1`, id)
	n, err := scanNode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, fmt.Errorf("getting node: %w", err)
	}
	return n, nil
}

func (s *PostgresStore) GetNodeByInstanceID(ctx context.Context, accountID, instanceID string) (Node, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+nodeColumns+` FROM nodes WHERE account_id = $1 AND instance_id = $2`,
		accountID, instanceID)
	n, err := scanNode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	if err != nil {
		return Node{}, fmt.Errorf("getting node by instance id: %w", err)
	}
	return n, nil
}

func (s *PostgresStore) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+nodeColumns+` FROM nodes ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	defer rows.Close()

	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning node: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListNodesByAccount(ctx context.Context, accountID string) ([]Node, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+nodeColumns+` FROM nodes WHERE account_id = $1 ORDER BY created_at`, accountID)
	if err != nil {
		return nil, fmt.Errorf("listing nodes by account: %w", err)
	}
	defer rows.Close()

	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning node: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNode(row rowScanner) (Node, error) {
	var n Node
	var capsRaw []byte
	var instanceID *string
	err := row.Scan(&n.ID, &n.AccountID, &instanceID, &n.Hostname, &n.Arch, &n.CPUFlags, &capsRaw,
		&n.OfferCores, &n.OfferMemoryMB, &n.OfferDiskMB, &n.BenchScore, &n.TrustScore,
		&n.Connected, &n.LastHeartbeatAt, &n.CreatedAt)
	if err != nil {
		return Node{}, err
	}
	if instanceID != nil {
		n.InstanceID = *instanceID
	}
	if len(capsRaw) > 0 {
		if err := json.Unmarshal(capsRaw, &n.Capabilities); err != nil {
			return Node{}, fmt.Errorf("unmarshalling capabilities: %w", err)
		}
	}
	return n, nil
}

// nullIfEmpty coalesces "" to SQL NULL: instance_id's partial unique index
// excludes NULL specifically so nodes that never report one (shouldn't
// happen outside tests) don't collide with each other under it.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *PostgresStore) SetNodeConnected(ctx context.Context, id string, connected bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE nodes SET connected = $2 WHERE id = $1`, id, connected)
	if err != nil {
		return fmt.Errorf("setting node connected: %w", err)
	}
	return nil
}

func (s *PostgresStore) RecordHeartbeat(ctx context.Context, id string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE nodes SET last_heartbeat_at = $2 WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("recording heartbeat: %w", err)
	}
	return nil
}

func (s *PostgresStore) SetNodeOffer(ctx context.Context, id string, cores float64, memoryMB, diskMB int) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE nodes SET offer_cores = $2, offer_memory_mb = $3, offer_disk_mb = $4 WHERE id = $1`,
		id, cores, memoryMB, diskMB)
	if err != nil {
		return fmt.Errorf("setting node offer: %w", err)
	}
	return nil
}

func (s *PostgresStore) SetNodeBenchScore(ctx context.Context, id string, score float64) error {
	_, err := s.pool.Exec(ctx, `UPDATE nodes SET bench_score = $2 WHERE id = $1`, id, score)
	if err != nil {
		return fmt.Errorf("setting bench score: %w", err)
	}
	return nil
}

func (s *PostgresStore) SetNodeTrustScore(ctx context.Context, id string, score float64) error {
	_, err := s.pool.Exec(ctx, `UPDATE nodes SET trust_score = $2 WHERE id = $1`, id, score)
	if err != nil {
		return fmt.Errorf("setting trust score: %w", err)
	}
	return nil
}
