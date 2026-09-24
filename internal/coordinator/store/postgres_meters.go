package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) RecordMeterStarted(ctx context.Context, taskID, nodeID string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO task_meters (task_id, node_id, started_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (task_id) DO NOTHING`,
		taskID, nodeID, at)
	if err != nil {
		return fmt.Errorf("recording meter started: %w", err)
	}
	return nil
}

func (s *PostgresStore) RecordMeterFinished(ctx context.Context, taskID string, at time.Time, durationS, normalisedS float64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE task_meters SET finished_at = $2, duration_s = $3, normalised_s = $4
		WHERE task_id = $1`,
		taskID, at, durationS, normalisedS)
	if err != nil {
		return fmt.Errorf("recording meter finished: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) GetMeter(ctx context.Context, taskID string) (TaskMeter, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT task_id, node_id, started_at, finished_at, duration_s, normalised_s
		FROM task_meters WHERE task_id = $1`, taskID)
	var m TaskMeter
	err := row.Scan(&m.TaskID, &m.NodeID, &m.StartedAt, &m.FinishedAt, &m.DurationS, &m.NormalisedS)
	if errors.Is(err, pgx.ErrNoRows) {
		return TaskMeter{}, ErrNotFound
	}
	if err != nil {
		return TaskMeter{}, fmt.Errorf("getting meter: %w", err)
	}
	return m, nil
}
