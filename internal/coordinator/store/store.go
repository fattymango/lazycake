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
