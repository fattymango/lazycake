package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) RecordNodeUsage(ctx context.Context, u NodeUsageSample) error {
	at := u.At
	if at.IsZero() {
		at = time.Now()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning usage record: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once committed

	// Only the tasks this node is actually running.
	ids := make([]string, 0, len(u.Tasks))
	for _, t := range u.Tasks {
		ids = append(ids, t.TaskID)
	}
	owned := map[string]bool{}
	if len(ids) > 0 {
		rows, err := tx.Query(ctx, `SELECT id FROM tasks WHERE id = ANY($1) AND node_id = $2`, ids, u.NodeID)
		if err != nil {
			return fmt.Errorf("checking which tasks belong to the node: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("scanning task id: %w", err)
			}
			owned[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("checking which tasks belong to the node: %w", err)
		}
	}
	var tasks []TaskUsageSample
	var tasksCPU, tasksMem float64
	for _, t := range u.Tasks {
		if owned[t.TaskID] {
			tasks = append(tasks, t)
			tasksCPU += t.CPUCores
			tasksMem += float64(t.MemoryBytes)
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO node_usage (node_id, bucket, samples, cpu_busy_sum, cpu_count, mem_used_sum, mem_total,
		                        disk_used_sum, disk_total, tasks_cpu_sum, tasks_mem_sum)
		VALUES ($1, `+usageBucketSQL+`, 1, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (node_id, bucket) DO UPDATE SET
			samples       = node_usage.samples + 1,
			cpu_busy_sum  = node_usage.cpu_busy_sum + EXCLUDED.cpu_busy_sum,
			cpu_count     = EXCLUDED.cpu_count,
			mem_used_sum  = node_usage.mem_used_sum + EXCLUDED.mem_used_sum,
			mem_total     = EXCLUDED.mem_total,
			disk_used_sum = node_usage.disk_used_sum + EXCLUDED.disk_used_sum,
			disk_total    = EXCLUDED.disk_total,
			tasks_cpu_sum = node_usage.tasks_cpu_sum + EXCLUDED.tasks_cpu_sum,
			tasks_mem_sum = node_usage.tasks_mem_sum + EXCLUDED.tasks_mem_sum`,
		u.NodeID, at, u.HostCPUBusy, u.HostCPUCount, float64(u.HostMemUsed), u.HostMemTotal,
		float64(u.DiskUsed), u.DiskTotal, tasksCPU, tasksMem); err != nil {
		return fmt.Errorf("updating machine usage bucket: %w", err)
	}

	for _, t := range tasks {
		if _, err := tx.Exec(ctx, `
			INSERT INTO node_task_usage (node_id, task_id, bucket, cpu_sum, mem_sum)
			VALUES ($1, $3, `+usageBucketSQL+`, $4, $5)
			ON CONFLICT (node_id, task_id, bucket) DO UPDATE SET
				cpu_sum = node_task_usage.cpu_sum + EXCLUDED.cpu_sum,
				mem_sum = node_task_usage.mem_sum + EXCLUDED.mem_sum`,
			u.NodeID, at, t.TaskID, t.CPUCores, float64(t.MemoryBytes)); err != nil {
			return fmt.Errorf("updating task usage bucket: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_usage_totals (task_id, node_id, core_seconds, peak_memory_bytes, tunnel_to_gateway, tunnel_to_task, samples)
			VALUES ($1, $2, $3, $4, $5, $6, 1)
			ON CONFLICT (task_id) DO UPDATE SET
				core_seconds      = task_usage_totals.core_seconds + EXCLUDED.core_seconds,
				peak_memory_bytes = GREATEST(task_usage_totals.peak_memory_bytes, EXCLUDED.peak_memory_bytes),
				tunnel_to_gateway = GREATEST(task_usage_totals.tunnel_to_gateway, EXCLUDED.tunnel_to_gateway),
				tunnel_to_task    = GREATEST(task_usage_totals.tunnel_to_task, EXCLUDED.tunnel_to_task),
				samples           = task_usage_totals.samples + 1,
				updated_at        = now()`,
			t.TaskID, u.NodeID, t.CPUCores*float64(u.IntervalMS)/1000, t.MemoryBytes, t.TunnelToGateway, t.TunnelToTask); err != nil {
			return fmt.Errorf("updating task usage summary: %w", err)
		}
	}

	latest := u
	latest.Tasks = tasks
	blob, err := json.Marshal(latest)
	if err != nil {
		return fmt.Errorf("encoding latest usage: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO node_usage_latest (node_id, at, sample) VALUES ($1, $2, $3)
		ON CONFLICT (node_id) DO UPDATE SET at = EXCLUDED.at, sample = EXCLUDED.sample`,
		u.NodeID, at, blob); err != nil {
		return fmt.Errorf("updating latest usage: %w", err)
	}
	return tx.Commit(ctx)
}

// usageBucketSQL rounds the timestamp parameter $2 down to its 5-minute bucket.
const usageBucketSQL = `date_bin('5 minutes', $2::timestamptz, TIMESTAMPTZ '2000-01-01')`

func (s *PostgresStore) NodeUsageLatest(ctx context.Context, nodeID string) (NodeUsageLatest, bool, error) {
	var at time.Time
	var blob []byte
	err := s.pool.QueryRow(ctx, `SELECT at, sample FROM node_usage_latest WHERE node_id = $1`, nodeID).Scan(&at, &blob)
	if errors.Is(err, pgx.ErrNoRows) {
		return NodeUsageLatest{}, false, nil
	}
	if err != nil {
		return NodeUsageLatest{}, false, fmt.Errorf("reading latest usage: %w", err)
	}
	var sample NodeUsageSample
	if err := json.Unmarshal(blob, &sample); err != nil {
		return NodeUsageLatest{}, false, fmt.Errorf("decoding latest usage: %w", err)
	}
	return NodeUsageLatest{At: at, Sample: sample}, true, nil
}

// usagePeriodSQL turns a step name into the expression that groups buckets.
func usagePeriodSQL(step string) (string, error) {
	switch step {
	case "5m":
		return "bucket", nil
	case "hour":
		return "date_trunc('hour', bucket)", nil
	}
	return "", fmt.Errorf("unknown usage step %q", step)
}

func (s *PostgresStore) NodeUsageSeries(ctx context.Context, nodeID string, since time.Time, step string) ([]NodeUsagePoint, error) {
	period, err := usagePeriodSQL(step)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+period+` AS p,
		       SUM(samples),
		       SUM(cpu_busy_sum)  / NULLIF(SUM(samples), 0),
		       MAX(cpu_count),
		       SUM(mem_used_sum)  / NULLIF(SUM(samples), 0),
		       MAX(mem_total),
		       SUM(disk_used_sum) / NULLIF(SUM(samples), 0),
		       MAX(disk_total),
		       SUM(tasks_cpu_sum) / NULLIF(SUM(samples), 0),
		       SUM(tasks_mem_sum) / NULLIF(SUM(samples), 0)
		FROM node_usage WHERE node_id = $1 AND bucket >= $2
		GROUP BY p ORDER BY p`, nodeID, since)
	if err != nil {
		return nil, fmt.Errorf("reading usage series: %w", err)
	}
	defer rows.Close()
	var out []NodeUsagePoint
	for rows.Next() {
		var p NodeUsagePoint
		var cpuBusy, memUsed, diskUsed, tasksCPU, tasksMem *float64
		if err := rows.Scan(&p.At, &p.Samples, &cpuBusy, &p.HostCPUCount, &memUsed, &p.HostMemTotal, &diskUsed, &p.DiskTotal, &tasksCPU, &tasksMem); err != nil {
			return nil, fmt.Errorf("scanning usage series: %w", err)
		}
		p.HostCPUBusy = deref(cpuBusy)
		p.HostMemUsed = int64(deref(memUsed))
		p.DiskUsed = int64(deref(diskUsed))
		p.TasksCPU = deref(tasksCPU)
		p.TasksMem = int64(deref(tasksMem))
		out = append(out, p)
	}
	return out, rows.Err()
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func (s *PostgresStore) NodeTaskSeries(ctx context.Context, nodeID string, since time.Time, step string) ([]NodeTaskPoint, error) {
	period, err := usagePeriodSQL(step)
	if err != nil {
		return nil, err
	}
	// A task's share is its sum over the machine's sample count for the same period, so a task
	// that ran for a third of the hour counts for a third of it.
	rows, err := s.pool.Query(ctx, `
		WITH n AS (
			SELECT `+period+` AS p, SUM(samples) AS samples FROM node_usage
			WHERE node_id = $1 AND bucket >= $2 GROUP BY p
		), t AS (
			SELECT `+period+` AS p, task_id, SUM(cpu_sum) AS cpu, SUM(mem_sum) AS mem FROM node_task_usage
			WHERE node_id = $1 AND bucket >= $2 GROUP BY p, task_id
		)
		SELECT t.p, t.task_id, t.cpu / NULLIF(n.samples, 0), t.mem / NULLIF(n.samples, 0)
		FROM t JOIN n USING (p) ORDER BY t.p, t.task_id`, nodeID, since)
	if err != nil {
		return nil, fmt.Errorf("reading task usage series: %w", err)
	}
	defer rows.Close()
	var out []NodeTaskPoint
	for rows.Next() {
		var p NodeTaskPoint
		var cpu, mem *float64
		if err := rows.Scan(&p.At, &p.TaskID, &cpu, &mem); err != nil {
			return nil, fmt.Errorf("scanning task usage series: %w", err)
		}
		p.CPUCores, p.MemoryBytes = deref(cpu), int64(deref(mem))
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) TaskUsageSummaries(ctx context.Context, taskIDs []string) (map[string]TaskUsageSummary, error) {
	out := map[string]TaskUsageSummary{}
	if len(taskIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT task_id, core_seconds, peak_memory_bytes, tunnel_to_gateway, tunnel_to_task
		FROM task_usage_totals WHERE task_id = ANY($1)`, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("reading task usage summaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var t TaskUsageSummary
		if err := rows.Scan(&id, &t.CoreSeconds, &t.PeakMemoryBytes, &t.TunnelToGateway, &t.TunnelToTask); err != nil {
			return nil, fmt.Errorf("scanning task usage summary: %w", err)
		}
		out[id] = t
	}
	return out, rows.Err()
}

func (s *PostgresStore) PruneNodeUsage(ctx context.Context, before time.Time) (int64, error) {
	a, err := s.pool.Exec(ctx, `DELETE FROM node_usage WHERE bucket < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("pruning machine usage: %w", err)
	}
	b, err := s.pool.Exec(ctx, `DELETE FROM node_task_usage WHERE bucket < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("pruning task usage: %w", err)
	}
	return a.RowsAffected() + b.RowsAffected(), nil
}
