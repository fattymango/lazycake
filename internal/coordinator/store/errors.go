package store

import "errors"

var (
	// ErrNotFound is returned by any Get* method when the row does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrNoTask is returned by ClaimQueuedTask when nothing matches the filter.
	ErrNoTask = errors.New("store: no matching queued task")
	// ErrConflict is returned by TransitionTask when the task is not in one
	// of the expected fromStates.
	ErrConflict = errors.New("store: task not in expected state")
	// ErrDuplicate is returned by CreateTask when idempotency_key collides.
	ErrDuplicate = errors.New("store: duplicate idempotency key")
)
