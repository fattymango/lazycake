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
	targets, err := json.Marshal(t.TunnelTargets)
	if err != nil {
		return fmt.Errorf("marshalling tunnel targets: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO tasks (
			id, account_id, idempotency_key, state,
			image, entrypoint, args, env, workdir,
			cpu_cores, memory_mb, disk_mb, tmpfs_mb, pids_limit,
			wall_timeout_s, no_output_timeout_s, egress_mb,
			req_arch, req_cpu_flags, req_isolation, req_confidentiality,
			gateway_ids, tunnel_targets, delivery, max_attempts, attempt
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8, $9,
			$10, $11, $12, $13, $14,
			$15, $16, $17,
			$18, $19, $20, $21,
			$22, $23, $24, $25, $26
		)`,
		t.ID, t.AccountID, t.IdempotencyKey, string(t.State),
		t.Image, orEmpty(t.Entrypoint), orEmpty(t.Args), env, t.Workdir,
		t.Limits.CPUCores, t.Limits.MemoryMB, t.Limits.DiskMB, t.Limits.TmpfsMB, t.Limits.PIDs,
		t.Limits.WallTimeoutS, t.Limits.NoOutputTimeoutS, t.Limits.EgressMB,
		t.Requirements.Arch, orEmpty(t.Requirements.CPUFlags), t.Requirements.Isolation, t.Requirements.Confidentiality,
		orEmpty(t.GatewayIDs), targets, string(t.Delivery), t.Retry.MaxAttempts, t.Attempt,
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
	gateway_ids, tunnel_targets, delivery, max_attempts, attempt,
	node_id, lease_expires_at, requeue_after,
	exit_code, exit_reason, started_at, finished_at, created_at, cancel_requested_at`

func scanTask(row rowScanner) (Task, error) {
	var t Task
	var state, delivery string
	var envRaw, targetsRaw []byte
	err := row.Scan(
		&t.ID, &t.AccountID, &t.IdempotencyKey, &state,
		&t.Image, &t.Entrypoint, &t.Args, &envRaw, &t.Workdir,
		&t.Limits.CPUCores, &t.Limits.MemoryMB, &t.Limits.DiskMB, &t.Limits.TmpfsMB, &t.Limits.PIDs,
		&t.Limits.WallTimeoutS, &t.Limits.NoOutputTimeoutS, &t.Limits.EgressMB,
		&t.Requirements.Arch, &t.Requirements.CPUFlags, &t.Requirements.Isolation, &t.Requirements.Confidentiality,
		&t.GatewayIDs, &targetsRaw, &delivery, &t.Retry.MaxAttempts, &t.Attempt,
		&t.NodeID, &t.LeaseExpiresAt, &t.RequeueAfter,
		&t.ExitCode, &t.ExitReason, &t.StartedAt, &t.FinishedAt, &t.CreatedAt, &t.CancelRequestedAt,
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
	if len(targetsRaw) > 0 {
		if err := json.Unmarshal(targetsRaw, &t.TunnelTargets); err != nil {
			return Task{}, fmt.Errorf("unmarshalling tunnel targets: %w", err)
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

func (s *PostgresStore) ListRecentTasks(ctx context.Context, limit int) ([]Task, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+taskColumns+` FROM tasks ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("listing recent tasks: %w", err)
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

func (s *PostgresStore) ListTasksByAccount(ctx context.Context, accountID string, limit int) ([]Task, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE account_id = $1 ORDER BY created_at DESC LIMIT $2`,
		accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing tasks by account: %w", err)
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
				AND ($8::timestamptz IS NULL OR created_at < $8 OR ($10::int > 0 AND wall_timeout_s <= $10::int))
				AND NOT (id = ANY($9::text[]))
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING `+taskColumns,
		nodeID, requeueAfter,
		filter.Arch, filter.Isolations, filter.FreeCores, filter.FreeMemoryMB, filter.FreeDiskMB,
		nullTime(filter.OlderThan), nonNilStrings(filter.ExcludeTaskIDs), filter.BackfillWallS,
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

func (s *PostgresStore) RequeueTaskForRetry(ctx context.Context, id string, fromStates []TaskState) error {
	fromStrs := make([]string, len(fromStates))
	for i, st := range fromStates {
		fromStrs[i] = string(st)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE tasks SET
			state = 'queued', node_id = NULL, lease_expires_at = NULL, requeue_after = NULL,
			attempt = attempt + 1
		WHERE id = $1 AND state = ANY($2)`,
		id, fromStrs)
	if err != nil {
		return fmt.Errorf("requeueing task for retry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s *PostgresStore) ReleaseReservedTask(ctx context.Context, id string, fromStates []TaskState) error {
	fromStrs := make([]string, len(fromStates))
	for i, st := range fromStates {
		fromStrs[i] = string(st)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE tasks SET
			state = 'queued', node_id = NULL, lease_expires_at = NULL, requeue_after = NULL
		WHERE id = $1 AND state = ANY($2)`,
		id, fromStrs)
	if err != nil {
		return fmt.Errorf("releasing reserved task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s *PostgresStore) AbandonTask(ctx context.Context, id string, fromStates []TaskState, at time.Time) error {
	fromStrs := make([]string, len(fromStates))
	for i, st := range fromStates {
		fromStrs[i] = string(st)
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE tasks SET state = 'abandoned', finished_at = $3
		WHERE id = $1 AND state = ANY($2)`,
		id, fromStrs, at)
	if err != nil {
		return fmt.Errorf("abandoning task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

func (s *PostgresStore) ExtendNodeRequeue(ctx context.Context, nodeID string, newRequeueAfter time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE tasks SET requeue_after = $2
		WHERE node_id = $1 AND state IN ('dispatched','running')
			AND (requeue_after IS NULL OR requeue_after < $2)`,
		nodeID, newRequeueAfter)
	if err != nil {
		return fmt.Errorf("extending node requeue: %w", err)
	}
	return nil
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

// RequestTaskCancel records that the customer asked for the task to be
// stopped, but only while it is still active, so a task that finishes in the
// same instant is never marked. It reports whether the task was still active
// (and so now carries the request); calling it again keeps the original time.
func (s *PostgresStore) RequestTaskCancel(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE tasks SET cancel_requested_at = COALESCE(cancel_requested_at, now())
		WHERE id = $1 AND state IN ('queued','reserved','dispatched','running')`, id)
	if err != nil {
		return false, fmt.Errorf("requesting task cancel: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (s *PostgresStore) ListCancelRequested(ctx context.Context) ([]Task, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE cancel_requested_at IS NOT NULL AND state IN ('dispatched','running')`)
	if err != nil {
		return nil, fmt.Errorf("listing cancel-requested tasks: %w", err)
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

func (s *PostgresStore) AccountTaskSummary(ctx context.Context, accountID string, finishedSince time.Time) (TaskSummary, error) {
	sum := TaskSummary{ByState: map[TaskState]int{}, FinishedByState: map[TaskState]int{}, Gateways: map[string]GatewayTaskSummary{}}

	rows, err := s.pool.Query(ctx, `
		SELECT state::text, count(*), COALESCE(sum(cpu_cores) FILTER (WHERE state IN ('dispatched','running')), 0)::float8
		FROM tasks WHERE account_id = $1 GROUP BY state`, accountID)
	if err != nil {
		return sum, fmt.Errorf("counting tasks: %w", err)
	}
	for rows.Next() {
		var st string
		var n int
		var cores float64
		if err := rows.Scan(&st, &n, &cores); err != nil {
			rows.Close()
			return sum, fmt.Errorf("scanning task counts: %w", err)
		}
		sum.ByState[TaskState(st)] = n
		sum.RunningCores += cores
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sum, err
	}

	frows, err := s.pool.Query(ctx, `
		SELECT state::text, count(*) FROM tasks
		WHERE account_id = $1 AND state NOT IN ('queued','reserved','dispatched','running')
			AND COALESCE(finished_at, created_at) >= $2
		GROUP BY state`, accountID, finishedSince)
	if err != nil {
		return sum, fmt.Errorf("reading recent outcomes: %w", err)
	}
	for frows.Next() {
		var st string
		var n int
		if err := frows.Scan(&st, &n); err != nil {
			frows.Close()
			return sum, err
		}
		sum.FinishedByState[TaskState(st)] = n
	}
	frows.Close()
	if err := frows.Err(); err != nil {
		return sum, err
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM tasks
		WHERE account_id = $1 AND cardinality(gateway_ids) > 0 AND state IN ('queued','reserved','dispatched','running')`,
		accountID).Scan(&sum.UsingGateways); err != nil {
		return sum, fmt.Errorf("counting tasks using gateways: %w", err)
	}

	grows, err := s.pool.Query(ctx, `
		SELECT gid,
		       count(*) FILTER (WHERE state IN ('dispatched','running')),
		       count(*) FILTER (WHERE state IN ('queued','reserved')),
		       (array_agg(id ORDER BY created_at DESC))[1],
		       (extract(epoch FROM max(created_at)) * 1000)::bigint
		FROM tasks, unnest(gateway_ids) AS gid
		WHERE account_id = $1
		GROUP BY gid`, accountID)
	if err != nil {
		return sum, fmt.Errorf("summarising gateway use: %w", err)
	}
	defer grows.Close()
	for grows.Next() {
		var gid string
		var g GatewayTaskSummary
		if err := grows.Scan(&gid, &g.Running, &g.Queued, &g.LastTaskID, &g.LastTaskAtMS); err != nil {
			return sum, fmt.Errorf("scanning gateway use: %w", err)
		}
		sum.Gateways[gid] = g
	}
	return sum, grows.Err()
}

func (s *PostgresStore) OldestQueuedBefore(ctx context.Context, offer CapacityFilter, before time.Time) (Task, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks
		WHERE state = 'queued'
			AND req_arch = $1
			AND req_isolation = ANY($2)
			AND cpu_cores <= $3
			AND memory_mb <= $4
			AND disk_mb <= $5
			AND created_at < $6
		ORDER BY created_at
		LIMIT 1`,
		offer.Arch, offer.Isolations, offer.FreeCores, offer.FreeMemoryMB, offer.FreeDiskMB, before)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNoTask
	}
	if err != nil {
		return Task{}, fmt.Errorf("finding the oldest waiting task: %w", err)
	}
	return t, nil
}

// nullTime is SQL NULL for the zero time, so "no limit" needs no special query.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// nonNilStrings turns a nil slice into an empty one (a nil Go slice is SQL NULL, and `id = ANY(NULL)` is never true).
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *PostgresStore) NodeReservations(ctx context.Context, nodeIDs []string) (map[string]Reservation, error) {
	out := map[string]Reservation{}
	if len(nodeIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT node_id, COALESCE(sum(cpu_cores), 0)::float8, COALESCE(sum(memory_mb), 0)::int
		FROM tasks WHERE node_id = ANY($1) AND state IN ('reserved','dispatched','running')
		GROUP BY node_id`, nodeIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var r Reservation
		if err := rows.Scan(&id, &r.Cores, &r.MemoryMB); err != nil {
			return nil, err
		}
		out[id] = r
	}
	return out, rows.Err()
}
