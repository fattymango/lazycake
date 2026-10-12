package portalapi

import (
	"net/http"
	"time"

	"github.com/fattymango/lazycake/internal/coordinator/store"
)

// finishedWindow is how far back the success rate looks.
const finishedWindow = 7 * 24 * time.Hour

type gatewayTaskView struct {
	Running      int    `json:"running"`
	Queued       int    `json:"queued"`
	LastTaskID   string `json:"last_task_id,omitempty"`
	LastTaskAtMS int64  `json:"last_task_at_ms,omitempty"`
}

type taskSummaryResponse struct {
	// ByState counts all of the account's tasks, not just the newest page of them.
	ByState map[string]int `json:"by_state"`
	// Running is dispatched plus running; Queued is queued plus reserved.
	Running      int     `json:"running"`
	Queued       int     `json:"queued"`
	RunningCores float64 `json:"running_cores"`
	// Finished is how many tasks finished in the last 7 days (any outcome) and how many of them succeeded.
	Finished          int `json:"finished"`
	FinishedSucceeded int `json:"finished_succeeded"`
	// FinishedByState is the same window by outcome (succeeded, failed, cancelled = stopped by the customer, fenced, abandoned), for the hover on the ring.
	FinishedByState map[string]int             `json:"finished_by_state"`
	UsingGateways   int                        `json:"using_gateways"`
	Gateways        map[string]gatewayTaskView `json:"gateways"`
}

// handleTaskSummary is GET .../tasks/summary: counts over every one of the account's tasks. The Overview and the gateway pages show these
// instead of counting the capped list of newest tasks, which under a backlog showed "0 running, nothing finished".
func (s *Server) handleTaskSummary(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	sum, err := s.Store.AccountTaskSummary(r.Context(), sess.AccountID, time.Now().Add(-finishedWindow))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "summarising tasks")
		return
	}
	resp := taskSummaryResponse{
		ByState: map[string]int{}, FinishedByState: map[string]int{}, Gateways: map[string]gatewayTaskView{},
		Running:      sum.ByState[store.TaskRunning] + sum.ByState[store.TaskDispatched],
		Queued:       sum.ByState[store.TaskQueued] + sum.ByState[store.TaskReserved],
		RunningCores: sum.RunningCores, UsingGateways: sum.UsingGateways,
	}
	for st, n := range sum.ByState {
		resp.ByState[string(st)] = n
	}
	for st, n := range sum.FinishedByState {
		resp.Finished += n
		resp.FinishedByState[string(st)] = n
		if st == store.TaskSucceeded {
			resp.FinishedSucceeded += n
		}
	}
	for id, g := range sum.Gateways {
		resp.Gateways[id] = gatewayTaskView{Running: g.Running, Queued: g.Queued, LastTaskID: g.LastTaskID, LastTaskAtMS: g.LastTaskAtMS}
	}
	writeJSON(w, http.StatusOK, resp)
}
