package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) CreateTask(ctx context.Context, t Task) error {
	env, err := json.Marshal(t.Env)
	if err != nil {
		return fmt.Errorf("marshalling env: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO tasks (
			id, account_id, idempotency_key, state,
			image, entrypoint, args, env, workdir,
			cpu_cores, memory_mb, disk_mb, tmpfs_mb, pids_limit,
			wall_timeout_s, no_output_timeout_s, egress_mb,
			req_arch, req_cpu_flags, req_isolation, req_confidentiality,
			gateway_ids, delivery, max_attempts, attempt
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8, $9,
			$10, $11, $12, $13, $14,
			$15, $16, $17,
			$18, $19, $20, $21,
			$22, $23, $24, $25
		)`,
		t.ID, t.AccountID, t.IdempotencyKey, string(t.State),
		t.Image, orEmpty(t.Entrypoint), orEmpty(t.Args), env, t.Workdir,
		t.Limits.CPUCores, t.Limits.MemoryMB, t.Limits.DiskMB, t.Limits.TmpfsMB, t.Limits.PIDs,
		t.Limits.WallTimeoutS, t.Limits.NoOutputTimeoutS, t.Limits.EgressMB,
		t.Requirements.Arch, orEmpty(t.Requirements.CPUFlags), t.Requirements.Isolation, t.Requirements.Confidentiality,
		orEmpty(t.GatewayIDs), string(t.Delivery), t.Retry.MaxAttempts, t.Attempt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return fmt.Errorf("creating task: %w", err)
	}
	return nil
}

const taskColumns = `
	id, account_id, idempotency_key, state,
	image, entrypoint, args, env, workdir,
	cpu_cores, memory_mb, disk_mb, tmpfs_mb, pids_limit,
	wall_timeout_s, no_output_timeout_s, egress_mb,
	req_arch, req_cpu_flags, req_isolation, req_confidentiality,
	gateway_ids, delivery, max_attempts, attempt,
	node_id, lease_expires_at, requeue_after,
	exit_code, exit_reason, started_at, finished_at, created_at`

func scanTask(row rowScanner) (Task, error) {
	var t Task
	var state, delivery string
	var envRaw []byte
	err := row.Scan(
		&t.ID, &t.AccountID, &t.IdempotencyKey, &state,
		&t.Image, &t.Entrypoint, &t.Args, &envRaw, &t.Workdir,
		&t.Limits.CPUCores, &t.Limits.MemoryMB, &t.Limits.DiskMB, &t.Limits.TmpfsMB, &t.Limits.PIDs,
		&t.Limits.WallTimeoutS, &t.Limits.NoOutputTimeoutS, &t.Limits.EgressMB,
		&t.Requirements.Arch, &t.Requirements.CPUFlags, &t.Requirements.Isolation, &t.Requirements.Confidentiality,
		&t.GatewayIDs, &delivery, &t.Retry.MaxAttempts, &t.Attempt,
		&t.NodeID, &t.LeaseExpiresAt, &t.RequeueAfter,
		&t.ExitCode, &t.ExitReason, &t.StartedAt, &t.FinishedAt, &t.CreatedAt,
	)
	if err != nil {
		return Task{}, err
	}
	t.State = TaskState(state)
	t.Delivery = DeliveryMode(delivery)
	if len(envRaw) > 0 {
		if err := json.Unmarshal(envRaw, &t.Env); err != nil {
			return Task{}, fmt.Errorf("unmarshalling env: %w", err)
		}
	}
	return t, nil
}

func (s *PostgresStore) GetTask(ctx context.Context, id string) (Task, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = $1`, id)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("getting task: %w", err)
	}
	return t, nil
}

func (s *PostgresStore) ListTasksByNode(ctx context.Context, nodeID string, states []TaskState) ([]Task, error) {
	stateStrs := make([]string, len(states))
	for i, st := range states {
		stateStrs[i] = string(st)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE node_id = $1 AND state = ANY($2)`,
		nodeID, stateStrs)
	if err != nil {
		return nil, fmt.Errorf("listing tasks by node: %w", err)
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ClaimQueuedTask implements the SELECT ... FOR UPDATE SKIP LOCKED pattern
// from IMPLEMENTATION.md task 1.2: multiple coordinator replicas can call
// this concurrently and each queued task is claimed by exactly one of them.
func (s *PostgresStore) ClaimQueuedTask(ctx context.Context, nodeID string, filter CapacityFilter, requeueAfter time.Time) (Task, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE tasks SET state = 'reserved', node_id = $1, requeue_after = $2
		WHERE id = (
			SELECT id FROM tasks
			WHERE state = 'queued'
				AND req_arch = $3
				AND req_isolation = ANY($4)
				AND cpu_cores <= $5
				AND memory_mb <= $6
				AND disk_mb <= $7
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING `+taskColumns,
		nodeID, requeueAfter,
		filter.Arch, filter.Isolations, filter.FreeCores, filter.FreeMemoryMB, filter.FreeDiskMB,
	)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNoTask
	}
	if err != nil {
		return Task{}, fmt.Errorf("claiming task: %w", err)
	}
	return t, nil
}

func (s *PostgresStore) TransitionTask(ctx context.Context, id string, fromStates []TaskState, to TaskState, upd TaskUpdate) error {
	fromStrs := make([]string, len(fromStates))
	for i, st := range fromStates {
		fromStrs[i] = string(st)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE tasks SET
			state = $3,
			node_id = COALESCE($4, node_id),
			lease_expires_at = COALESCE($5, lease_expires_at),
			requeue_after = COALESCE($6, requeue_after),
			exit_code = COALESCE($7, exit_code),
			exit_reason = COALESCE($8, exit_reason),
			started_at = COALESCE($9, started_at),
			finished_at = COALESCE($10, finished_at),
			attempt = COALESCE($11, attempt)
		WHERE id = $1 AND state = ANY($2)`,
		id, fromStrs, string(to),
		upd.NodeID, upd.LeaseExpiresAt, upd.RequeueAfter,
		upd.ExitCode, upd.ExitReason, upd.StartedAt, upd.FinishedAt, upd.Attempt,
	)
	if err != nil {
		return fmt.Errorf("transitioning task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s *PostgresStore) RequeueOverdue(ctx context.Context, now time.Time) ([]Task, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+taskColumns+` FROM tasks
		 WHERE requeue_after IS NOT NULL AND requeue_after <= $1
			AND state IN ('reserved','dispatched','running')`, now)
	if err != nil {
		return nil, fmt.Errorf("listing overdue tasks: %w", err)
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// orEmpty coalesces a nil slice to an empty one: passing nil as a pgx array
// parameter binds SQL NULL, not '{}', which the NOT NULL columns here reject.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}
