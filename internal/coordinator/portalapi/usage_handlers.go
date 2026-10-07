package portalapi

import (
	"net/http"
	"sort"
	"time"
)

// Machine usage (task 8.15). Display only: the host controls the agent that reports
// these numbers, so nothing here may feed billing or trust.

// usageTopTasks is how many tasks get their own band in the stacked chart; the rest
// are folded into one "others" band so a busy machine stays readable.
const usageTopTasks = 8

// usageOthersKey is the series key of that folded band.
const usageOthersKey = "_others"

type usageShare struct {
	CPUCores    float64 `json:"cpu_cores"`
	MemoryBytes int64   `json:"memory_bytes"`
}

type usagePointView struct {
	AtMS        int64                 `json:"at_ms"`
	Samples     int                   `json:"samples"`
	HostCPUBusy float64               `json:"host_cpu_busy"`
	HostMemUsed int64                 `json:"host_mem_used_bytes"`
	DiskUsed    int64                 `json:"disk_used_bytes"`
	TasksCPU    float64               `json:"tasks_cpu_cores"`
	TasksMemory int64                 `json:"tasks_memory_bytes"`
	PerTask     map[string]usageShare `json:"per_task"`
}

type usageLatestView struct {
	AtMS         int64        `json:"at_ms"`
	HostCPUBusy  float64      `json:"host_cpu_busy"`
	HostCPUCount int          `json:"host_cpu_count"`
	HostMemTotal int64        `json:"host_mem_total_bytes"`
	HostMemUsed  int64        `json:"host_mem_used_bytes"`
	DiskTotal    int64        `json:"disk_total_bytes"`
	DiskUsed     int64        `json:"disk_used_bytes"`
	TasksCPU     float64      `json:"tasks_cpu_cores"`
	TasksMemory  int64        `json:"tasks_memory_bytes"`
	Tasks        []latestTask `json:"tasks"`
}

type latestTask struct {
	TaskID      string  `json:"task_id"`
	CPUCores    float64 `json:"cpu_cores"`
	MemoryBytes int64   `json:"memory_bytes"`
}

type nodeUsageResponse struct {
	// Supported is false until the machine's agent has reported usage at least once
	// (an agent built before this feature never does): the page then says to update it.
	Supported bool             `json:"supported"`
	Range     string           `json:"range"`
	Step      string           `json:"step"`
	Latest    *usageLatestView `json:"latest,omitempty"`
	// Tasks lists the series keys that have their own band, biggest first. A point's
	// per_task also holds usageOthersKey when more tasks than that were running.
	Tasks  []string         `json:"tasks"`
	Series []usagePointView `json:"series"`
}

// handleNodeUsage is GET .../nodes/{id}/usage?range=24h|7d.
func (s *Server) handleNodeUsage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	nodeID := r.PathValue("id")
	if _, err := s.getOwnNode(r.Context(), sess.AccountID, nodeID); err != nil {
		writeStoreOrRPCError(w, err)
		return
	}
	rng, step, window := "24h", "5m", 24*time.Hour
	switch r.URL.Query().Get("range") {
	case "", "24h":
	case "7d":
		rng, step, window = "7d", "hour", 7*24*time.Hour
	default:
		writeError(w, http.StatusBadRequest, "range must be 24h or 7d")
		return
	}

	resp := nodeUsageResponse{Range: rng, Step: step, Tasks: []string{}, Series: []usagePointView{}}
	latest, ok, err := s.Store.NodeUsageLatest(r.Context(), nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading usage")
		return
	}
	if ok {
		resp.Supported = true
		l := latest.Sample
		lv := &usageLatestView{
			AtMS: latest.At.UnixMilli(), HostCPUBusy: l.HostCPUBusy, HostCPUCount: l.HostCPUCount,
			HostMemTotal: l.HostMemTotal, HostMemUsed: l.HostMemUsed, DiskTotal: l.DiskTotal, DiskUsed: l.DiskUsed,
			Tasks: []latestTask{},
		}
		for _, t := range l.Tasks {
			lv.TasksCPU += t.CPUCores
			lv.TasksMemory += t.MemoryBytes
			lv.Tasks = append(lv.Tasks, latestTask{TaskID: t.TaskID, CPUCores: t.CPUCores, MemoryBytes: t.MemoryBytes})
		}
		resp.Latest = lv
	}

	since := time.Now().Add(-window)
	points, err := s.Store.NodeUsageSeries(r.Context(), nodeID, since, step)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading usage")
		return
	}
	taskPoints, err := s.Store.NodeTaskSeries(r.Context(), nodeID, since, step)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading usage")
		return
	}
	if len(points) > 0 {
		resp.Supported = true
	}

	// The biggest consumers over the window get their own band.
	total := map[string]float64{}
	for _, tp := range taskPoints {
		total[tp.TaskID] += tp.CPUCores
	}
	ids := make([]string, 0, len(total))
	for id := range total {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if total[ids[i]] != total[ids[j]] {
			return total[ids[i]] > total[ids[j]]
		}
		return ids[i] < ids[j]
	})
	named := map[string]bool{}
	for i, id := range ids {
		if i < usageTopTasks {
			named[id] = true
			resp.Tasks = append(resp.Tasks, id)
		}
	}

	for _, p := range points {
		pv := usagePointView{
			AtMS: p.At.UnixMilli(), Samples: p.Samples, HostCPUBusy: p.HostCPUBusy, HostMemUsed: p.HostMemUsed,
			DiskUsed: p.DiskUsed, TasksCPU: p.TasksCPU, TasksMemory: p.TasksMem, PerTask: map[string]usageShare{},
		}
		resp.Series = append(resp.Series, pv)
	}
	// Series holds copies, so fill the per-task maps through an index.
	idx := map[int64]int{}
	for i, pv := range resp.Series {
		idx[pv.AtMS] = i
	}
	for _, tp := range taskPoints {
		i, ok := idx[tp.At.UnixMilli()]
		if !ok {
			continue
		}
		key := tp.TaskID
		if !named[key] {
			key = usageOthersKey
		}
		cur := resp.Series[i].PerTask[key]
		cur.CPUCores += tp.CPUCores
		cur.MemoryBytes += tp.MemoryBytes
		resp.Series[i].PerTask[key] = cur
	}
	writeJSON(w, http.StatusOK, resp)
}

type taskUsageView struct {
	CoreSeconds     float64 `json:"core_seconds"`
	PeakMemoryBytes int64   `json:"peak_memory_bytes"`
	TunnelToGateway int64   `json:"tunnel_bytes_to_gateway"`
	TunnelToTask    int64   `json:"tunnel_bytes_to_task"`
}

// nodeTaskView is a task on a machine, with what it used when the agent reported it.
type nodeTaskView struct {
	taskView
	Usage *taskUsageView `json:"usage,omitempty"`
}
