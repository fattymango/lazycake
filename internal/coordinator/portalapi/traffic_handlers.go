package portalapi

import (
	"net/http"
	"time"
)

// Gateway and task traffic (task 8.14). Display only: nothing here feeds billing.
// Labels from the customer's side: "received from tasks" is what the gateway
// handed to the local service (bytes_to_local); "sent to tasks" is what came
// back from it (bytes_to_task).

type trafficTotalsView struct {
	ReceivedFromTasksBytes int64 `json:"received_from_tasks_bytes"`
	SentToTasksBytes       int64 `json:"sent_to_tasks_bytes"`
	Connections            int64 `json:"connections"`
}

type trafficPointView struct {
	AtMS                   int64 `json:"at_ms"`
	ReceivedFromTasksBytes int64 `json:"received_from_tasks_bytes"`
	SentToTasksBytes       int64 `json:"sent_to_tasks_bytes"`
}

type busiestTaskView struct {
	TaskID string `json:"task_id"`
	trafficTotalsView
}

type gatewayTrafficResponse struct {
	Range   string             `json:"range"`
	Step    string             `json:"step"`
	Totals  trafficTotalsView  `json:"totals"`
	Series  []trafficPointView `json:"series"`
	Busiest []busiestTaskView  `json:"busiest_tasks"`
}

type taskTrafficRow struct {
	GatewayID string `json:"gateway_id"`
	Service   string `json:"service"`
	trafficTotalsView
}

type taskTrafficResponse struct {
	Totals trafficTotalsView `json:"totals"`
	Rows   []taskTrafficRow  `json:"rows"`
}

// handleGatewayTraffic is GET .../gateways/{id}/traffic?range=7d|30d.
func (s *Server) handleGatewayTraffic(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	gw, err := s.Store.GetGateway(r.Context(), r.PathValue("id"))
	if err != nil || gw.AccountID != sess.AccountID {
		writeError(w, http.StatusNotFound, "gateway not found")
		return
	}
	rng, step, window := "7d", "hour", 7*24*time.Hour
	switch r.URL.Query().Get("range") {
	case "", "7d":
	case "30d":
		rng, step, window = "30d", "day", 30*24*time.Hour
	default:
		writeError(w, http.StatusBadRequest, "range must be 7d or 30d")
		return
	}
	since := time.Now().Add(-window)
	totals, err := s.Store.GatewayTotals(r.Context(), []string{gw.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading traffic")
		return
	}
	points, err := s.Store.GatewayTrafficSeries(r.Context(), gw.ID, since, step)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading traffic")
		return
	}
	busiest, err := s.Store.GatewayBusiestTasks(r.Context(), gw.ID, since, 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading traffic")
		return
	}
	resp := gatewayTrafficResponse{Range: rng, Step: step, Totals: totalsView(totals[gw.ID].BytesToLocal, totals[gw.ID].BytesToTask, totals[gw.ID].Connections),
		Series: []trafficPointView{}, Busiest: []busiestTaskView{}}
	for _, p := range points {
		resp.Series = append(resp.Series, trafficPointView{AtMS: p.At.UnixMilli(), ReceivedFromTasksBytes: p.BytesToLocal, SentToTasksBytes: p.BytesToTask})
	}
	for _, b := range busiest {
		resp.Busiest = append(resp.Busiest, busiestTaskView{TaskID: b.TaskID, trafficTotalsView: totalsView(b.BytesToLocal, b.BytesToTask, b.Connections)})
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleTaskTraffic is GET .../tasks/{id}/traffic: what the task moved through each gateway service.
func (s *Server) handleTaskTraffic(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	task, err := s.getOwnTask(r.Context(), sess.AccountID, r.PathValue("id"))
	if err != nil {
		writeStoreOrRPCError(w, err)
		return
	}
	rows, err := s.Store.TaskGatewayUsage(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading traffic")
		return
	}
	resp := taskTrafficResponse{Rows: []taskTrafficRow{}}
	for _, row := range rows {
		resp.Rows = append(resp.Rows, taskTrafficRow{GatewayID: row.GatewayID, Service: row.Service, trafficTotalsView: totalsView(row.BytesToLocal, row.BytesToTask, row.Connections)})
		resp.Totals.ReceivedFromTasksBytes += row.BytesToLocal
		resp.Totals.SentToTasksBytes += row.BytesToTask
		resp.Totals.Connections += row.Connections
	}
	writeJSON(w, http.StatusOK, resp)
}

func totalsView(toLocal, toTask, conns int64) trafficTotalsView {
	return trafficTotalsView{ReceivedFromTasksBytes: toLocal, SentToTasksBytes: toTask, Connections: conns}
}
