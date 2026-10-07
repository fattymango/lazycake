package store

import (
	"context"
	"fmt"
	"time"
)

// trafficBucketSQL rounds a timestamp down to its 5-minute bucket.
const trafficBucketSQL = `date_bin('5 minutes', $1::timestamptz, TIMESTAMPTZ '2000-01-01')`

func (s *PostgresStore) RecordGatewayTraffic(ctx context.Context, r GatewayTrafficReport) error {
	at := r.At
	if at.IsZero() {
		at = time.Now()
	}
	conns := r.Connections
	if conns == 0 && r.Final {
		conns = 1
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning traffic report: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once committed

	if _, err := tx.Exec(ctx, `
		INSERT INTO gateway_totals (gateway_id, bytes_to_local, bytes_to_task, connections)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (gateway_id) DO UPDATE SET
			bytes_to_local = gateway_totals.bytes_to_local + EXCLUDED.bytes_to_local,
			bytes_to_task  = gateway_totals.bytes_to_task  + EXCLUDED.bytes_to_task,
			connections    = gateway_totals.connections    + EXCLUDED.connections,
			updated_at     = now()`,
		r.GatewayID, r.BytesToLocal, r.BytesToTask, conns); err != nil {
		return fmt.Errorf("updating gateway totals: %w", err)
	}

	if r.RecordTask {
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_gateway_totals (task_id, gateway_id, service, bytes_to_local, bytes_to_task, connections)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (task_id, gateway_id, service) DO UPDATE SET
				bytes_to_local = task_gateway_totals.bytes_to_local + EXCLUDED.bytes_to_local,
				bytes_to_task  = task_gateway_totals.bytes_to_task  + EXCLUDED.bytes_to_task,
				connections    = task_gateway_totals.connections    + EXCLUDED.connections,
				updated_at     = now()`,
			r.TaskID, r.GatewayID, r.Service, r.BytesToLocal, r.BytesToTask, conns); err != nil {
			return fmt.Errorf("updating task totals: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO gateway_traffic (gateway_id, task_id, service, bucket, bytes_to_local, bytes_to_task, connections)
			VALUES ($2, $3, $4, `+trafficBucketSQL+`, $5, $6, $7)
			ON CONFLICT (gateway_id, task_id, service, bucket) DO UPDATE SET
				bytes_to_local = gateway_traffic.bytes_to_local + EXCLUDED.bytes_to_local,
				bytes_to_task  = gateway_traffic.bytes_to_task  + EXCLUDED.bytes_to_task,
				connections    = gateway_traffic.connections    + EXCLUDED.connections`,
			at, r.GatewayID, r.TaskID, r.Service, r.BytesToLocal, r.BytesToTask, conns); err != nil {
			return fmt.Errorf("updating traffic bucket: %w", err)
		}
	}
	if r.RecordTask {
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_gateway_samples (task_id, gateway_id, at, bytes_to_local, bytes_to_task)
			VALUES ($1, $5, date_bin('10 seconds', $2::timestamptz, TIMESTAMPTZ '2000-01-01'), $3, $4)
			ON CONFLICT (task_id, gateway_id, at) DO UPDATE SET
				bytes_to_local = task_gateway_samples.bytes_to_local + EXCLUDED.bytes_to_local,
				bytes_to_task  = task_gateway_samples.bytes_to_task  + EXCLUDED.bytes_to_task`,
			r.TaskID, at, r.BytesToLocal, r.BytesToTask, r.GatewayID); err != nil {
			return fmt.Errorf("updating task gateway sample: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) GatewayTotals(ctx context.Context, gatewayIDs []string) (map[string]TrafficTotals, error) {
	out := map[string]TrafficTotals{}
	if len(gatewayIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT gateway_id, bytes_to_local, bytes_to_task, connections FROM gateway_totals WHERE gateway_id = ANY($1)`, gatewayIDs)
	if err != nil {
		return nil, fmt.Errorf("reading gateway totals: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var t TrafficTotals
		if err := rows.Scan(&id, &t.BytesToLocal, &t.BytesToTask, &t.Connections); err != nil {
			return nil, fmt.Errorf("scanning gateway totals: %w", err)
		}
		out[id] = t
	}
	return out, rows.Err()
}

func (s *PostgresStore) GatewayTrafficSeries(ctx context.Context, gatewayID string, since time.Time, step string) ([]TrafficPoint, error) {
	if step != "hour" && step != "day" {
		return nil, fmt.Errorf("unknown step %q", step)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT date_trunc('`+step+`', bucket) AS at, SUM(bytes_to_local)::bigint, SUM(bytes_to_task)::bigint
		FROM gateway_traffic WHERE gateway_id = $1 AND bucket >= $2
		GROUP BY 1 ORDER BY 1`, gatewayID, since)
	if err != nil {
		return nil, fmt.Errorf("reading gateway traffic series: %w", err)
	}
	defer rows.Close()
	var out []TrafficPoint
	for rows.Next() {
		var p TrafficPoint
		if err := rows.Scan(&p.At, &p.BytesToLocal, &p.BytesToTask); err != nil {
			return nil, fmt.Errorf("scanning traffic point: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GatewayBusiestTasks(ctx context.Context, gatewayID string, since time.Time, limit int) ([]TaskTraffic, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT task_id, SUM(bytes_to_local)::bigint, SUM(bytes_to_task)::bigint, SUM(connections)::bigint
		FROM gateway_traffic WHERE gateway_id = $1 AND bucket >= $2
		GROUP BY task_id
		ORDER BY SUM(bytes_to_local) + SUM(bytes_to_task) DESC, task_id
		LIMIT $3`, gatewayID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("reading busiest tasks: %w", err)
	}
	defer rows.Close()
	var out []TaskTraffic
	for rows.Next() {
		var t TaskTraffic
		if err := rows.Scan(&t.TaskID, &t.BytesToLocal, &t.BytesToTask, &t.Connections); err != nil {
			return nil, fmt.Errorf("scanning busiest task: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *PostgresStore) TaskGatewayUsage(ctx context.Context, taskID string) ([]TaskGatewayTraffic, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT gateway_id, service, bytes_to_local, bytes_to_task, connections
		FROM task_gateway_totals WHERE task_id = $1 ORDER BY gateway_id, service`, taskID)
	if err != nil {
		return nil, fmt.Errorf("reading task gateway usage: %w", err)
	}
	defer rows.Close()
	var out []TaskGatewayTraffic
	for rows.Next() {
		var t TaskGatewayTraffic
		if err := rows.Scan(&t.GatewayID, &t.Service, &t.BytesToLocal, &t.BytesToTask, &t.Connections); err != nil {
			return nil, fmt.Errorf("scanning task gateway usage: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *PostgresStore) PruneGatewayTraffic(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM gateway_traffic WHERE bucket < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("pruning gateway traffic: %w", err)
	}
	fine, err := s.pool.Exec(ctx, `DELETE FROM task_gateway_samples WHERE at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("pruning task gateway samples: %w", err)
	}
	return tag.RowsAffected() + fine.RowsAffected(), nil
}
