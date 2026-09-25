package store

import (
	"context"
	"time"
)

// Store is everything the coordinator persists. It is the seam between
// coordinator logic and Postgres: every package that needs persistence
// depends on this interface, constructed once in cmd/coordinator and passed
// down, so a fake implementation can stand in for unit tests and a
// different backend could replace PostgresStore without touching callers.
type Store interface {
	Accounts
	Tokens
	Nodes
	Tasks
	Logs
	ImageCache
	Gateways
	Meters
	Ledger
	Holds
	PortalAuth
}

type Accounts interface {
	CreateAccount(ctx context.Context, a Account) error
	GetAccount(ctx context.Context, id string) (Account, error)
	// AdjustBalance atomically adds deltaMicros (which may be negative) to
	// the account's balance and returns the resulting balance.
	AdjustBalance(ctx context.Context, id string, deltaMicros int64) (int64, error)
}

type Tokens interface {
	CreateToken(ctx context.Context, t APIToken) error
	// Authenticate looks up a token by its hash and returns the token row
	// if it exists and is not revoked.
	Authenticate(ctx context.Context, tokenHash []byte) (APIToken, error)
}

type Nodes interface {
	UpsertNode(ctx context.Context, n Node) error
	GetNode(ctx context.Context, id string) (Node, error)
	// GetNodeByInstanceID looks up a node by the stable per-process identity
	// an agent presents on every Register, so a reconnect can be recognised
	// as the same node (task 3.3). Returns ErrNotFound if none matches.
	GetNodeByInstanceID(ctx context.Context, accountID, instanceID string) (Node, error)
	ListNodes(ctx context.Context) ([]Node, error)
	// ListNodesByAccount returns only accountID's own nodes (task 7.3: the
	// provider portal's own machine list - nobody sees another account's
	// nodes through it, unlike the fleet-wide ListNodes the /ops dashboard
	// uses).
	ListNodesByAccount(ctx context.Context, accountID string) ([]Node, error)
	SetNodeConnected(ctx context.Context, id string, connected bool) error
	RecordHeartbeat(ctx context.Context, id string, at time.Time) error
	SetNodeOffer(ctx context.Context, id string, cores float64, memoryMB, diskMB int) error
	SetNodeBenchScore(ctx context.Context, id string, score float64) error
	SetNodeTrustScore(ctx context.Context, id string, score float64) error
}

type Tasks interface {
	CreateTask(ctx context.Context, t Task) error
	GetTask(ctx context.Context, id string) (Task, error)
	ListTasksByNode(ctx context.Context, nodeID string, states []TaskState) ([]Task, error)
	// ListRecentTasks returns the most recently created tasks fleet-wide,
	// newest first, for task 6.2's dashboard initial snapshot (the live
	// table itself is then kept current via task 6.1's SSE events).
	ListRecentTasks(ctx context.Context, limit int) ([]Task, error)
	// ListTasksByAccount returns accountID's own tasks, newest first, for
	// task 7.3's customer portal task history - nobody sees another
	// account's tasks through it.
	ListTasksByAccount(ctx context.Context, accountID string, limit int) ([]Task, error)

	// ClaimQueuedTask atomically picks one queued task matching filter,
	// moves it to 'reserved' with the given node and lease, and returns it.
	// Uses SELECT ... FOR UPDATE SKIP LOCKED so concurrent claimers never
	// race for the same task. Returns ErrNoTask if nothing matches.
	ClaimQueuedTask(ctx context.Context, nodeID string, filter CapacityFilter, requeueAfter time.Time) (Task, error)

	// TransitionTask moves a task to a new state, applying the given field
	// updates in the same statement. It fails if the task is not currently
	// in one of fromStates, so callers cannot race a state machine step.
	TransitionTask(ctx context.Context, id string, fromStates []TaskState, to TaskState, upd TaskUpdate) error

	// RequeueOverdue returns the IDs of tasks whose requeue_after has
	// passed while still in an active state, for the reclaimer loop.
	RequeueOverdue(ctx context.Context, now time.Time) ([]Task, error)

	// RequeueTaskForRetry moves an overdue at_least_once task back to
	// 'queued' with attempt incremented, clearing its node assignment and
	// lease so any connected node can claim it fresh - never COALESCE'd
	// like TransitionTask, since node_id/lease_expires_at/requeue_after
	// must actually become NULL here, not stay pointed at the node that
	// just missed its lease. Fails (ErrConflict) if id is not currently in
	// one of fromStates.
	RequeueTaskForRetry(ctx context.Context, id string, fromStates []TaskState) error

	// AbandonTask moves an overdue task to 'abandoned' - the terminal state
	// for a missed-lease at_most_once task, or an at_least_once task with
	// no attempts left (IMPLEMENTATION.md task 3.4). Fails (ErrConflict) if
	// id is not currently in one of fromStates.
	AbandonTask(ctx context.Context, id string, fromStates []TaskState, at time.Time) error

	// ExtendNodeRequeue pushes requeue_after forward (never backward) for
	// every dispatched/running task on nodeID, on every heartbeat the
	// coordinator receives from it - see PLAN.md "Lease and fencing" and
	// task 3.1's invariant: the coordinator measures from when it
	// *received* a heartbeat, which is never earlier than when the agent
	// sent it.
	ExtendNodeRequeue(ctx context.Context, nodeID string, newRequeueAfter time.Time) error
}

// TaskUpdate carries the optional fields TransitionTask may set alongside a
// state change. Nil fields are left unchanged.
type TaskUpdate struct {
	NodeID         *string
	LeaseExpiresAt *time.Time
	RequeueAfter   *time.Time
	ExitCode       *int
	ExitReason     *string
	StartedAt      *time.Time
	FinishedAt     *time.Time
	Attempt        *int
}

type Logs interface {
	AppendLogs(ctx context.Context, lines []LogLine) error
	ListLogs(ctx context.Context, taskID string, sinceSeq int64) ([]LogLine, error)
}

type ImageCache interface {
	RecordCachedImages(ctx context.Context, nodeID string, images []CachedImage) error
	RemoveCachedImages(ctx context.Context, nodeID string, digests []string) error
	ListCachedImages(ctx context.Context, nodeID string) ([]CachedImage, error)
	// NodesWithImage returns node IDs known to already hold digest, for
	// cache-aware placement.
	NodesWithImage(ctx context.Context, digest string) ([]string, error)
}

type Gateways interface {
	CreateGateway(ctx context.Context, g Gateway) error
	GetGateway(ctx context.Context, id string) (Gateway, error)
	ListGatewaysByAccount(ctx context.Context, accountID string) ([]Gateway, error)
	// SetGatewayConnected records connectedness and, when connected is
	// true, the Noise public key the gateway just published at connect
	// time (nil/empty otherwise - it isn't cleared on disconnect, so the
	// last-known key stays around for display/debugging).
	SetGatewayConnected(ctx context.Context, id string, connected bool, noisePubkey []byte) error
}

// Meters persists task_meters rows (IMPLEMENTATION.md task 4.2): duration
// and normalised duration computed purely from the coordinator's own
// receive-time clock, never from agent-reported timestamps.
type Meters interface {
	// RecordMeterStarted creates the row for taskID, capturing the
	// coordinator's own receive time for its TaskStarted event.
	RecordMeterStarted(ctx context.Context, taskID, nodeID string, at time.Time) error
	// RecordMeterFinished sets finished_at, duration_s and normalised_s on
	// an existing row. Fails with ErrNotFound if RecordMeterStarted was
	// never called for taskID (should not happen - the state machine
	// guarantees TaskStarted precedes TaskFinished).
	RecordMeterFinished(ctx context.Context, taskID string, at time.Time, durationS, normalisedS float64) error
	GetMeter(ctx context.Context, taskID string) (TaskMeter, error)
}

// Ledger persists ledger_entries and applies their balance effect
// (IMPLEMENTATION.md task 4.4).
type Ledger interface {
	// SettleTask writes one charge row (against customerAccountID) and
	// one credit row (against hostAccountID) for taskID, both for
	// priceMicros, and applies both balance deltas - all in one
	// transaction, so an account's ledger rows always sum to its balance
	// history. priceMicros must be >= 0 (the charge is stored negated,
	// the credit as-is).
	SettleTask(ctx context.Context, taskID, customerAccountID, hostAccountID string, priceMicros int64) error
	// LedgerEntriesForAccount lists every ledger row for an account,
	// oldest first.
	LedgerEntriesForAccount(ctx context.Context, accountID string) ([]LedgerEntry, error)
	// LedgerTotals sums every charge and credit ever recorded, fleet-wide -
	// task 6.2's dashboard "running total of charges and credits". Both
	// returned values are positive magnitudes (charges are stored negative
	// internally; this returns their absolute total).
	LedgerTotals(ctx context.Context) (totalChargesMicros, totalCreditsMicros int64, err error)
}

// Holds persists task_holds (task 4.5): "Deduct a hold at dispatch, settle
// at completion." A hold reserves part of an account's balance against one
// in-flight task without moving any money.
type Holds interface {
	// PlaceHold reserves amountMicros of accountID's balance against
	// taskID. Fails with ErrDuplicate if a hold already exists for taskID
	// (a task is only ever dispatched once at a time).
	PlaceHold(ctx context.Context, taskID, accountID string, amountMicros int64) error
	// ReleaseHold removes taskID's hold. A no-op, not an error, if none
	// exists - e.g. a task that never reached dispatch.
	ReleaseHold(ctx context.Context, taskID string) error
	// AvailableBalance returns accountID's balance_micros minus the sum
	// of its active holds.
	AvailableBalance(ctx context.Context, accountID string) (int64, error)
}

// PortalAuth persists portal_credentials and sessions (task 7.1/7.2): a
// human's username/password login for one of the two portals, and the
// cookie-backed sessions it issues. Entirely separate from Tokens - a
// password authenticates a human in a browser, a bearer token a machine
// caller, no shared code path (PLAN.md §2).
type PortalAuth interface {
	// CreatePortalCredential creates one row. Fails with ErrDuplicate if
	// username is already taken.
	CreatePortalCredential(ctx context.Context, c PortalCredential) error
	// GetPortalCredentialByUsername looks up a credential for login.
	// Returns ErrNotFound if no such username exists.
	GetPortalCredentialByUsername(ctx context.Context, username string) (PortalCredential, error)

	// CreateSession creates one row.
	CreateSession(ctx context.Context, s Session) error
	// GetSession looks up a session by the hash of its cookie ID. Returns
	// ErrNotFound if it doesn't exist, is revoked, or has expired -
	// callers never need to check those fields themselves.
	GetSession(ctx context.Context, idHash []byte) (Session, error)
	// RevokeSession marks a session revoked (logout). A no-op, not an
	// error, if it doesn't exist or is already revoked.
	RevokeSession(ctx context.Context, idHash []byte) error
}
