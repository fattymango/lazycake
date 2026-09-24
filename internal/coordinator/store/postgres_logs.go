package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AppendLogs inserts lines, skipping any (task_id, seq) already stored so a
// re-sent batch after a dropped ack is harmless.
func (s *PostgresStore) AppendLogs(ctx context.Context, lines []LogLine) error {
	if len(lines) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, l := range lines {
		batch.Queue(
			`INSERT INTO task_logs (task_id, seq, stream, at, line)
			 VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
			l.TaskID, l.Seq, l.Stream, l.At, l.Line)
	}
	results := s.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range lines {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("appending logs: %w", err)
		}
	}
	return nil
}

func (s *PostgresStore) ListLogs(ctx context.Context, taskID string, sinceSeq int64) ([]LogLine, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT task_id, seq, stream, at, line FROM task_logs
		 WHERE task_id = $1 AND seq > $2 ORDER BY seq`, taskID, sinceSeq)
	if err != nil {
		return nil, fmt.Errorf("listing logs: %w", err)
	}
	defer rows.Close()

	var out []LogLine
	for rows.Next() {
		var l LogLine
		if err := rows.Scan(&l.TaskID, &l.Seq, &l.Stream, &l.At, &l.Line); err != nil {
			return nil, fmt.Errorf("scanning log line: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
