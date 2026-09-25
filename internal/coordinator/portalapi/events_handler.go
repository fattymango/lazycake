package portalapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/events"
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

	ch, cancel := s.Bus.SubscribeAccount(sess.AccountID)
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
			writeSSEJSON(w, toEventView(ev))
			flusher.Flush()
		}
	}
}
