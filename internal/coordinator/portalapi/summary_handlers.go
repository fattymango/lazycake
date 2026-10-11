package portalapi

import (
	"net/http"

	"github.com/fattymango/lazycake/internal/coordinator/store"
)

// finishedWindow is how many recent finished tasks the success rate looks at.
const finishedWindow = 50

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
	// Finished is how many of the newest non-active tasks were looked at (up to FinishedWindow) and how many of them succeeded.
	Finished          int                        `json:"finished"`
	FinishedSucceeded int                        `json:"finished_succeeded"`
	UsingGateways     int                        `json:"using_gateways"`
	Gateways          map[string]gatewayTaskView `json:"gateways"`
}

// handleTaskSummary is GET .../tasks/summary: counts over every one of the account's tasks. The Overview and the gateway pages show these
// instead of counting the capped list of newest tasks, which under a backlog showed "0 running, nothing finished".
func (s *Server) handleTaskSummary(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	sum, err := s.Store.AccountTaskSummary(r.Context(), sess.AccountID, finishedWindow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "summarising tasks")
		return
	}
	resp := taskSummaryResponse{
		ByState: map[string]int{}, Gateways: map[string]gatewayTaskView{},
		Running:      sum.ByState[store.TaskRunning] + sum.ByState[store.TaskDispatched],
		Queued:       sum.ByState[store.TaskQueued] + sum.ByState[store.TaskReserved],
		RunningCores: sum.RunningCores, UsingGateways: sum.UsingGateways, Finished: len(sum.Finished),
	}
	for st, n := range sum.ByState {
		resp.ByState[string(st)] = n
	}
	for _, st := range sum.Finished {
		if st == store.TaskSucceeded {
			resp.FinishedSucceeded++
		}
	}
	for id, g := range sum.Gateways {
		resp.Gateways[id] = gatewayTaskView{Running: g.Running, Queued: g.Queued, LastTaskID: g.LastTaskID, LastTaskAtMS: g.LastTaskAtMS}
	}
	writeJSON(w, http.StatusOK, resp)
}
