// Package scheduler places queued tasks onto connected agents and tracks
// task lifecycle events reported back over the agent stream. It depends on
// the coordinator's api package only through two small interfaces it
// defines itself (Dispatcher, satisfied structurally by *api.Registry) so
// api never has to import scheduler and the two packages can change
// independently - the DI seam requested for this project.
package scheduler

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/mkassab215/lazycake/internal/clock"
	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/pricing"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

// Dispatcher delivers a message to a specific connected node.
// *api.Registry satisfies this structurally.
type Dispatcher interface {
	Send(nodeID string, msg *lazycakev1.CoordinatorMessage) error
}

// BillingEvents receives the same start/finish events TaskEvents does, for
// task duration metering (task 4.2 onward). *billing.Meters satisfies this
// structurally - scheduler never imports billing's own dependencies back.
type BillingEvents interface {
	OnTaskStarted(ctx context.Context, nodeID, taskID string) error
	OnTaskFinished(ctx context.Context, nodeID string, ev api.TaskFinishedEvent) error
}

type noopBilling struct{}

func (noopBilling) OnTaskStarted(context.Context, string, string) error                 { return nil }
func (noopBilling) OnTaskFinished(context.Context, string, api.TaskFinishedEvent) error { return nil }

// rejectCooldown is how long a task/node pair is avoided after a rejection,
// per IMPLEMENTATION.md task 1.8 ("do not re-offer it to that node for
// 30s"). Simplified to a task-level cooldown against any single node that
// rejected it, re-checked right after a claim rather than expressed in the
// SQL filter - see OPEN_QUESTIONS.md.
const rejectCooldown = 30 * time.Second

// leaseMargin is added to a task's own lease when computing requeue_after,
// so the coordinator's reclaim always fires after the agent's own fence
// (PLAN.md "Lease and fencing").
const leaseMargin = 15 * time.Second

// Scheduler places tasks and processes the events api.Server reports.
type Scheduler struct {
	Store    store.Store
	Dispatch Dispatcher
	Clock    clock.Clock
	Log      *slog.Logger
	// cancelSent remembers when each cancel-requested task was last nudged
	// (guarded by mu); see resendCancels.
	cancelSent map[string]time.Time
	// Billing may be nil (defaults to a no-op) until wired in - phase 4.
	Billing BillingEvents
	// Rates prices task 4.5's dispatch-time hold; zero value means "use
	// pricing.DefaultRates()" - see rates(). Should match whatever
	// cmd/coordinator wires into billing.Ledger and api.CustomerServer, so
	// the hold, the up-front affordability check, and the eventual charge
	// all agree.
	Rates pricing.Rates

	// SpecDrift is task 5.1's node-honesty check, fed one observation per
	// billable task finish (see OnTaskFinished). Always non-nil - New()
	// sets it.
	SpecDrift *SpecDriftTracker
	// Trust is task 5.3's per-node trust score, fed by SpecDrift, byte
	// reconciliation (task 4.3, wired in cmd/coordinator), canaries (task
	// 5.2) and this scheduler's own clean-completion/abandonment events.
	// Always non-nil - New() sets it.
	Trust *TrustTracker
	// Canary is task 5.2's detection half, fed by GatewayService.ReportBytes
	// (see cmd/coordinator). Always non-nil - New() sets it.
	Canary *CanaryTracker
	// ColdPull is task 5.4's fleet-wide cold-pull cap. Always non-nil -
	// New() sets it.
	ColdPull *ColdPullLimiter
	// Bus, if set, publishes task state changes for task 6.1's SSE
	// stream. A nil Bus is a valid no-op.
	Bus *events.Bus
	// Canary injection config (task 5.2): empty PlatformGatewayID disables
	// injection entirely (no platform account/gateway configured), which
	// is the default until cmd/coordinator sets these up.
	PlatformAccountID      string
	PlatformGatewayID      string
	CanaryImage            string
	CanaryEntrypoint       []string
	CanaryArgs             []string
	CanaryTargetHostname   string
	CanaryTargetPort       int32
	CanaryExpectedRuntimeS int
	// Rand returns a float in [0,1) for the canary injection roll;
	// defaults to math/rand's package-level source. Overridable so tests
	// can force (or forbid) injection deterministically.
	Rand func() float64

	// LeaseS is how long a dispatched task's lease is before the agent
	// self-fences if it hears nothing (phase 3 uses this fully; phase 1
	// just needs a value for requeue_after).
	LeaseS int32

	mu       sync.Mutex
	freeCap  map[string]store.CapacityFilter // node_id -> last reported free capacity
	rejected map[rejectKey]time.Time         // (node_id, task_id) -> cooldown expiry
}

type rejectKey struct {
	nodeID, taskID string
}

// New returns a ready Scheduler.
func New(st store.Store, dispatch Dispatcher, ck clock.Clock, log *slog.Logger, leaseS int32) *Scheduler {
	trust := NewTrustTracker()
	return &Scheduler{
		Store: st, Dispatch: dispatch, Clock: ck, Log: log, LeaseS: leaseS,
		freeCap:   make(map[string]store.CapacityFilter),
		rejected:  make(map[rejectKey]time.Time),
		SpecDrift: NewSpecDriftTracker(),
		Trust:     trust,
		Canary:    NewCanaryTracker(trust, log),
		ColdPull:  NewColdPullLimiter(),
	}
}

func (s *Scheduler) rand() float64 {
	if s.Rand != nil {
		return s.Rand()
	}
	return rand.Float64()
}

func (s *Scheduler) now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock.Now()
}

func (s *Scheduler) billing() BillingEvents {
	if s.Billing == nil {
		return noopBilling{}
	}
	return s.Billing
}

func (s *Scheduler) rates() pricing.Rates {
	if s.Rates == (pricing.Rates{}) {
		return pricing.DefaultRates()
	}
	return s.Rates
}

var _ api.TaskEvents = (*Scheduler)(nil)
var _ api.CapacityEvents = (*Scheduler)(nil)
