package billing

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// divergenceThreshold is task 4.3's own number: flag anything above 2%.
const divergenceThreshold = 0.02

// finalizeGrace is how long Reconciler waits after a task finishes before
// computing its divergence, giving the relay's and gateway's own reports -
// which land slightly after the agent's TaskFinished, since they depend on
// stream/connection teardown finishing on a different path - a chance to
// arrive first. Whatever hasn't arrived by then is treated as 0, which is
// exactly correct for a task that never opened a tunnel at all (all three
// counts are legitimately 0, divergence 0%, nothing flagged).
const finalizeGrace = 3 * time.Second

// Reconciliation is IMPLEMENTATION.md task 4.3's own metric: the spread
// between the largest and smallest of three independently-reported byte
// counts for the same task, as a fraction of the largest.
type Reconciliation struct {
	TaskID        string
	NodeID        string
	BytesAgent    int64
	BytesRelay    int64
	BytesGateway  int64
	DivergencePct float64
	Flagged       bool
}

// Reconcile computes the divergence metric for one task's three
// independently-reported byte counts. Pure and side-effect free - the
// live accumulation and logging (an agent reporting inflated counts should
// be flagged, an honest one should not) lives in Reconciler below.
func Reconcile(taskID, nodeID string, bytesAgent, bytesRelay, bytesGateway int64) Reconciliation {
	max := bytesAgent
	min := bytesAgent
	for _, v := range [...]int64{bytesRelay, bytesGateway} {
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}
	var pct float64
	if max > 0 {
		pct = float64(max-min) / float64(max)
	}
	return Reconciliation{
		TaskID: taskID, NodeID: nodeID,
		BytesAgent: bytesAgent, BytesRelay: bytesRelay, BytesGateway: bytesGateway,
		DivergencePct: pct, Flagged: pct > divergenceThreshold,
	}
}

type partialCounts struct {
	agent, relay, gateway int64
}

// Reconciler accumulates bytes_agent/bytes_relay/bytes_gateway for
// in-flight tasks as each of the three sources reports in, and computes +
// logs the divergence once a task finishes (after finalizeGrace, to give
// the relay's and gateway's reports a chance to land).
type Reconciler struct {
	// ResolveNodeID looks up the node ID that ran taskID, for logging.
	// *store.PostgresStore's GetTask satisfies a trivial adapter of this -
	// kept minimal and structural rather than depending on store.Store
	// directly, matching the DI pattern the rest of the coordinator uses.
	ResolveNodeID func(ctx context.Context, taskID string) (string, error)
	Log           Logger

	mu      sync.Mutex
	pending map[string]*partialCounts
}

// Logger is the minimal slog.Logger surface Reconciler needs.
type Logger interface {
	Warn(msg string, args ...any)
	Info(msg string, args ...any)
}

func (r *Reconciler) counts(taskID string) *partialCounts {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending = make(map[string]*partialCounts)
	}
	c, ok := r.pending[taskID]
	if !ok {
		c = &partialCounts{}
		r.pending[taskID] = c
	}
	return c
}

// RecordAgentBytes records bytes_agent from a task's TaskFinished message
// (bytes_sent + bytes_recv).
func (r *Reconciler) RecordAgentBytes(taskID string, bytes int64) {
	c := r.counts(taskID)
	r.mu.Lock()
	c.agent = bytes
	r.mu.Unlock()
}

// RecordRelayBytes accumulates bytes_relay from one relayed stream closing
// (internal/tunnel/quic.Relay.OnStreamClosed) - a task may open more than
// one stream (one per tunnel target), so this adds rather than overwrites.
func (r *Reconciler) RecordRelayBytes(taskID string, bytes int64) {
	c := r.counts(taskID)
	r.mu.Lock()
	c.relay += bytes
	r.mu.Unlock()
}

// RecordGatewayBytes accumulates bytes_gateway from one forwarded
// connection closing (internal/gateway/listener.Listener.OnForward, via
// the ReportBytes RPC) - same accumulate-don't-overwrite reasoning as
// RecordRelayBytes.
func (r *Reconciler) RecordGatewayBytes(taskID string, bytes int64) {
	c := r.counts(taskID)
	r.mu.Lock()
	c.gateway += bytes
	r.mu.Unlock()
}

// Finalize schedules taskID's divergence computation after finalizeGrace,
// then discards its accumulated state. Call this once, from OnTaskFinished.
func (r *Reconciler) Finalize(taskID string) {
	time.AfterFunc(finalizeGrace, func() { r.finalizeNow(taskID) })
}

func (r *Reconciler) finalizeNow(taskID string) {
	r.mu.Lock()
	c, ok := r.pending[taskID]
	delete(r.pending, taskID)
	r.mu.Unlock()
	if !ok {
		return
	}

	nodeID := ""
	if r.ResolveNodeID != nil {
		if id, err := r.ResolveNodeID(context.Background(), taskID); err == nil {
			nodeID = id
		}
	}

	rec := Reconcile(taskID, nodeID, c.agent, c.relay, c.gateway)
	if rec.Flagged {
		r.Log.Warn("byte reconciliation divergence exceeds threshold",
			"task_id", taskID, "node_id", nodeID,
			"bytes_agent", rec.BytesAgent, "bytes_relay", rec.BytesRelay, "bytes_gateway", rec.BytesGateway,
			"divergence_pct", fmt.Sprintf("%.2f", rec.DivergencePct*100))
		return
	}
	r.Log.Info("byte reconciliation within threshold",
		"task_id", taskID, "node_id", nodeID, "divergence_pct", fmt.Sprintf("%.2f", rec.DivergencePct*100))
}
