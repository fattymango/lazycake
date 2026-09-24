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
	ID              string
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

	GatewayIDs []string
	Delivery   DeliveryMode
	Retry      Retry
	Attempt    int

	NodeID         *string
	LeaseExpiresAt *time.Time
	RequeueAfter   *time.Time

	ExitCode   *int
	ExitReason *string
	StartedAt  *time.Time
	FinishedAt *time.Time
	CreatedAt  time.Time
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

// CapacityFilter narrows ClaimQueuedTask to tasks a specific node can run.
type CapacityFilter struct {
	Arch         string
	Isolations   []string // any of these req_isolation values match
	FreeCores    float64
	FreeMemoryMB int
	FreeDiskMB   int
}
