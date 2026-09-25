package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) PlaceHold(ctx context.Context, taskID, accountID string, amountMicros int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO task_holds (task_id, account_id, amount_micros) VALUES ($1, $2, $3)`,
		taskID, accountID, amountMicros)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("placing hold: %w", err)
	}
	return nil
}

func (s *PostgresStore) ReleaseHold(ctx context.Context, taskID string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM task_holds WHERE task_id = $1`, taskID); err != nil {
		return fmt.Errorf("releasing hold: %w", err)
	}
	return nil
}

func (s *PostgresStore) AvailableBalance(ctx context.Context, accountID string) (int64, error) {
	var balance int64
	err := s.pool.QueryRow(ctx, `SELECT balance_micros FROM accounts WHERE id = $1`, accountID).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("getting balance: %w", err)
	}

	var held int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount_micros), 0) FROM task_holds WHERE account_id = $1`, accountID,
	).Scan(&held); err != nil {
		return 0, fmt.Errorf("summing holds: %w", err)
	}

	return balance - held, nil
}
