package portalapi

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/fattymango/lazycake/internal/coordinator/events"
	"github.com/fattymango/lazycake/internal/coordinator/store"
)

// sseKeepalive is how often handleAccountEvents writes a no-op comment
// line to an otherwise-idle stream. Without this, a quiet account (no
// task/node activity for a while) sits with zero bytes flowing, which is
// exactly what an idle-connection timeout somewhere in the chain - a
// browser, an intermediate proxy (this stream goes through Vite's dev
// proxy in front of the coordinator, unlike internal/coordinator/
// dashboard's own /events, which the browser hits directly) - will close,
// triggering EventSource's auto-reconnect and making the UI flap between
// live and "reconnecting" forever even though nothing is actually wrong.
// Same fix, same reasoning, as internal/tunnel/quic's KeepAlivePeriod.
const sseKeepalive = 15 * time.Second

// eventView is the wire shape of one SSE frame on the account-scoped
// stream, matching web/src/shared/types.ts's PortalEvent - the same
// events.Event task 6.1 built, filtered to the caller's own account
// (PLAN.md §3/§5.4) rather than broadcast fleet-wide the way the /ops
// dashboard's /events is.
type eventView struct {
	Type         string  `json:"type"`
	AtMS         int64   `json:"at_ms"`
	TaskID       string  `json:"task_id,omitempty"`
	State        string  `json:"state,omitempty"`
	NodeID       string  `json:"node_id,omitempty"`
	FreeCores    float64 `json:"free_cores,omitempty"`
	FreeMemoryMB int32   `json:"free_memory_mb,omitempty"`
	FreeDiskMB   int32   `json:"free_disk_mb,omitempty"`
}

func toEventView(e events.Event) eventView {
	return eventView{
		Type: e.Type, AtMS: e.AtMS, TaskID: e.TaskID, State: e.State, NodeID: e.NodeID,
		FreeCores: e.FreeCores, FreeMemoryMB: e.FreeMemoryMB, FreeDiskMB: e.FreeDiskMB,
	}
}

// handleAccountEvents implements the appendix's GET
// /api/portal/{customer,provider}/events: shared by both roles' routes
// since all it needs is the caller's own account_id, already resolved by
// requireSession regardless of which role required it.
func (s *Server) handleAccountEvents(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch, cancel, mine := s.eventStreamFor(r.Context(), sess)
	defer cancel()

	ticker := time.NewTicker(sseKeepalive)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if !mine(ev) {
				continue
			}
			writeSSEJSON(w, toEventView(ev))
			flusher.Flush()
		}
	}
}

// eventStreamFor picks what a session sees. A task's events carry its customer's account, so a provider, who is a different account, would never
// hear that a customer's task started or finished on the provider's own machine: the machine page only updated on reload. A provider therefore
// also gets the task_state events of tasks on its machines (state and ids only, which is what its machine page lists anyway).
func (s *Server) eventStreamFor(ctx context.Context, sess sessionInfo) (<-chan events.Event, func(), func(events.Event) bool) {
	if sess.Role != store.RoleProvider {
		ch, cancel := s.Bus.SubscribeAccount(sess.AccountID)
		return ch, cancel, func(events.Event) bool { return true }
	}
	ch, cancel := s.Bus.Subscribe()
	owned := map[string]bool{}
	checked := map[string]bool{}
	refresh := func() {
		nodes, err := s.Store.ListNodesByAccount(ctx, sess.AccountID)
		if err != nil {
			return
		}
		for _, n := range nodes {
			owned[n.ID] = true
		}
	}
	refresh()
	return ch, cancel, func(ev events.Event) bool {
		if ev.AccountID == sess.AccountID {
			return true
		}
		if ev.Type != "task_state" || ev.NodeID == "" {
			return false
		}
		if !owned[ev.NodeID] && !checked[ev.NodeID] {
			checked[ev.NodeID] = true // a machine registered after this stream opened: look once
			refresh()
		}
		return owned[ev.NodeID]
	}
}
