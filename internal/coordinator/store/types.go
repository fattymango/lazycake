// Package store defines the coordinator's persistence seam (the Store
// interface) and a Postgres-backed implementation of it. Every other
// coordinator package depends on the interface, never on *PostgresStore or
// on pgx directly, so a fake or an alternate backend can be swapped in for
// tests or future backends without touching callers.
package store

import (
	"time"
)

// TaskState is one of the task_state enum values in migrations/003_tasks.sql.
type TaskState string

const (
	TaskQueued     TaskState = "queued"
	TaskReserved   TaskState = "reserved"
	TaskDispatched TaskState = "dispatched"
	TaskRunning    TaskState = "running"
	TaskSucceeded  TaskState = "succeeded"
	TaskFailed     TaskState = "failed"
	TaskFenced     TaskState = "fenced"
	TaskAbandoned  TaskState = "abandoned"
	TaskCancelled  TaskState = "cancelled"
)

// DeliveryMode controls retry behaviour. See PLAN.md "Task descriptor".
type DeliveryMode string

const (
	AtMostOnce  DeliveryMode = "at_most_once"
	AtLeastOnce DeliveryMode = "at_least_once"
)

// Account is a customer or host's billing identity. The same account row
// covers both roles; a host and a customer just hold different token kinds.
type Account struct {
	ID            string
	Name          string
	BalanceMicros int64
	CreatedAt     time.Time
}

// TokenKind matches the api_tokens.kind CHECK constraint.
type TokenKind string

const (
	TokenCustomer TokenKind = "customer"
	TokenAgent    TokenKind = "agent"
	TokenGateway  TokenKind = "gateway"
)

// APIToken authenticates a caller as belonging to an account with a role.
type APIToken struct {
	TokenHash []byte
	AccountID string
	Kind      TokenKind
	CreatedAt time.Time
	RevokedAt *time.Time
}

// Capabilities mirrors lazycakev1.Capabilities: what an agent's preflight
// probe proved it can actually enforce.
type Capabilities struct {
	MemoryLimit   bool   `json:"memory_limit"`
	CPUQuota      bool   `json:"cpu_quota"`
	PIDsLimit     bool   `json:"pids_limit"`
	DiskLimit     bool   `json:"disk_limit"`
	GVisor        bool   `json:"gvisor"`
	SystemdSlice  bool   `json:"systemd_slice"`
	Runtime       string `json:"runtime"`
	CgroupVersion string `json:"cgroup_version"`
}

// Node is a host's registered machine.
type Node struct {
	ID string
	// InstanceID identifies one agent process's lifetime (minted fresh on
	// every process start, stable across that process's reconnects). It's
	// how a reconnecting agent is recognised as the same node rather than
	// getting a brand new node_id - and with it a fresh, empty set of
	// assigned tasks - every time (IMPLEMENTATION.md task 3.3).
	InstanceID      string
	AccountID       string
	Hostname        string
	Arch            string
	CPUFlags        []string
	Capabilities    Capabilities
	OfferCores      float64
	OfferMemoryMB   int
	OfferDiskMB     int
	BenchScore      *float64
	TrustScore      float64
	Connected       bool
	LastHeartbeatAt *time.Time
	CreatedAt       time.Time
}

// Limits mirrors the task descriptor's "limits" object in PLAN.md.
type Limits struct {
	CPUCores         float64
	MemoryMB         int
	DiskMB           int
	TmpfsMB          int
	PIDs             int
	WallTimeoutS     int
	NoOutputTimeoutS int
	EgressMB         int
}

// Requirements mirrors the task descriptor's "requires" object.
type Requirements struct {
	Arch            string
	CPUFlags        []string
	Isolation       string
	Confidentiality string
}

// Retry mirrors the task descriptor's "retry" object.
type Retry struct {
	MaxAttempts int
}

// Task is one unit of dispatchable work.
type Task struct {
	ID             string
	AccountID      string
	IdempotencyKey *string
	State          TaskState

	Image      string
	Entrypoint []string
	Args       []string
	Env        map[string]string
	Workdir    string

	Limits       Limits
	Requirements Requirements

	// GatewayIDs is a derived quick-query convenience list (the gateway
	// IDs referenced in TunnelTargets); TunnelTargets is the source of
	// truth used to actually build a Dispatch message's tunnel targets.
	GatewayIDs    []string
	TunnelTargets []TunnelTarget
	Delivery      DeliveryMode
	Retry         Retry
	Attempt       int

	NodeID         *string
	LeaseExpiresAt *time.Time
	RequeueAfter   *time.Time

	ExitCode   *int
	ExitReason *string
	StartedAt  *time.Time
	FinishedAt *time.Time
	CreatedAt  time.Time

	// CancelRequestedAt is when the customer asked for this task to be
	// stopped (nil if they never did). See Store.RequestTaskCancel.
	CancelRequestedAt *time.Time
}

// LogLine is one line of task output, ordered by Seq within a task.
type LogLine struct {
	TaskID string
	Seq    int64
	Stream string // "stdout" | "stderr"
	At     time.Time
	Line   string
}

// CachedImage is one digest an agent reported holding locally.
type CachedImage struct {
	NodeID    string
	Digest    string
	SizeBytes int64
	LastUsed  time.Time
}

// TunnelTarget is one hostname a task's container may resolve and reach
// through a gateway, mirroring lazycakev1.TunnelTargetSpec.
type TunnelTarget struct {
	GatewayID string `json:"gateway_id"`
	Hostname  string `json:"hostname"`
	Port      int32  `json:"port"`
}

// CapacityFilter narrows ClaimQueuedTask to tasks a specific node can run.
type CapacityFilter struct {
	Arch         string
	Isolations   []string // any of these req_isolation values match
	FreeCores    float64
	FreeMemoryMB int
	FreeDiskMB   int
}

// GatewayService is one local TCP service a gateway is willing to forward
// to, mirroring internal/gateway/config.Service.
type GatewayService struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

// TaskMeter is one task_meters row (task 4.2): duration_s/normalised_s are
// nil until RecordMeterFinished runs. NodeID is nil once the node that ran
// this task has since been deleted (migration 015 - the FK sets it NULL
// rather than blocking the delete or losing the meter row).
type TaskMeter struct {
	TaskID      string
	NodeID      *string
	StartedAt   time.Time
	FinishedAt  *time.Time
	DurationS   *float64
	NormalisedS *float64
}

// LedgerEntry is one ledger_entries row (task 4.4): a charge (negative
// AmountMicros) or a credit (positive AmountMicros) against one account,
// for one task.
type LedgerEntry struct {
	ID           string
	TaskID       string
	AccountID    string
	Kind         string // "charge" | "credit"
	AmountMicros int64
	CreatedAt    time.Time
}

// PortalRole is one of the two portal_credentials.role/sessions.role CHECK
// values (IMPLEMENTATION.md task 7.1) - fixed at signup by which portal an
// account registered on, never a per-request choice.
type PortalRole string

const (
	RoleCustomer PortalRole = "customer"
	RoleProvider PortalRole = "provider"
)

// PortalCredential is one portal_credentials row: a human's login for one
// of the two portals, at most one per account.
type PortalCredential struct {
	AccountID    string
	Username     string
	PasswordHash string
	Role         PortalRole
	CreatedAt    time.Time
}

// Session is one sessions row: a signed-in portal login, looked up by the
// hash of the random ID a browser holds as its session cookie.
type Session struct {
	IDHash    []byte
	AccountID string
	Role      PortalRole
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Gateway is a customer-installed relay endpoint, registered via
// `lcctl gateway create` before the gateway process itself ever runs -
// NoisePubkey starts empty and is filled in the first time it connects
// (see internal/tunnel/quic.Relay's GatewayRegistry).
type Gateway struct {
	ID          string
	AccountID   string
	Label       string
	NoisePubkey []byte
	Services    []GatewayService
	Connected   bool
	CreatedAt   time.Time
}
