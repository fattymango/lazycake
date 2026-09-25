package store

import (
	"context"
	"fmt"

	"github.com/mkassab215/lazycake/internal/id"
)

func (s *PostgresStore) SettleTask(ctx context.Context, taskID, customerAccountID, hostAccountID string, priceMicros int64) error {
	if priceMicros < 0 {
		return fmt.Errorf("settling task %s: priceMicros must be >= 0, got %d", taskID, priceMicros)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning settlement transaction: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once committed

	if _, err := tx.Exec(ctx,
		`INSERT INTO ledger_entries (id, task_id, account_id, kind, amount_micros) VALUES ($1, $2, $3, 'charge', $4)`,
		id.New("ldg"), taskID, customerAccountID, -priceMicros,
	); err != nil {
		return fmt.Errorf("recording charge: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE accounts SET balance_micros = balance_micros - $2 WHERE id = $1`,
		customerAccountID, priceMicros,
	); err != nil {
		return fmt.Errorf("debiting customer account: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO ledger_entries (id, task_id, account_id, kind, amount_micros) VALUES ($1, $2, $3, 'credit', $4)`,
		id.New("ldg"), taskID, hostAccountID, priceMicros,
	); err != nil {
		return fmt.Errorf("recording credit: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE accounts SET balance_micros = balance_micros + $2 WHERE id = $1`,
		hostAccountID, priceMicros,
	); err != nil {
		return fmt.Errorf("crediting host account: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing settlement: %w", err)
	}
	return nil
}

func (s *PostgresStore) LedgerEntriesForAccount(ctx context.Context, accountID string) ([]LedgerEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, task_id, account_id, kind, amount_micros, created_at
		 FROM ledger_entries WHERE account_id = $1 ORDER BY created_at`, accountID)
	if err != nil {
		return nil, fmt.Errorf("listing ledger entries: %w", err)
	}
	defer rows.Close()

	var out []LedgerEntry
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.TaskID, &e.AccountID, &e.Kind, &e.AmountMicros, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning ledger entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *PostgresStore) LedgerTotals(ctx context.Context) (totalChargesMicros, totalCreditsMicros int64, err error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			COALESCE(-SUM(amount_micros) FILTER (WHERE kind = 'charge'), 0),
			COALESCE(SUM(amount_micros) FILTER (WHERE kind = 'credit'), 0)
		FROM ledger_entries`)
	if err := row.Scan(&totalChargesMicros, &totalCreditsMicros); err != nil {
		return 0, 0, fmt.Errorf("summing ledger totals: %w", err)
	}
	return totalChargesMicros, totalCreditsMicros, nil
}
