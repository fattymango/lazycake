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
	// Bytes the task moved through its tunnel during the period (sums, not averages).
	TunnelOutBytes int64 `json:"tunnel_out_bytes"`
	TunnelInBytes  int64 `json:"tunnel_in_bytes"`
}

type usagePointView struct {
	AtMS        int64   `json:"at_ms"`
	Samples     int     `json:"samples"`
	HostCPUBusy float64 `json:"host_cpu_busy"`
	HostMemUsed int64   `json:"host_mem_used_bytes"`
	DiskUsed    int64   `json:"disk_used_bytes"`
	TasksCPU    float64 `json:"tasks_cpu_cores"`
	TasksMemory int64   `json:"tasks_memory_bytes"`
	// All tasks' tunnel bytes during the period: out of the containers, and into them.
	TunnelOutBytes int64                 `json:"tunnel_out_bytes"`
	TunnelInBytes  int64                 `json:"tunnel_in_bytes"`
	PerTask        map[string]usageShare `json:"per_task"`
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

	// The biggest consumers over the window get their own band: the top few by CPU, plus the
	// top few by tunnel traffic, so a task that is quiet on CPU but heavy on network isn't
	// hidden in "others" on the network chart.
	cpuTotal, netTotal := map[string]float64{}, map[string]int64{}
	for _, tp := range taskPoints {
		cpuTotal[tp.TaskID] += tp.CPUCores
		netTotal[tp.TaskID] += tp.TunnelOut + tp.TunnelIn
	}
	byCPU := make([]string, 0, len(cpuTotal))
	for id := range cpuTotal {
		byCPU = append(byCPU, id)
	}
	sort.Slice(byCPU, func(i, j int) bool {
		if cpuTotal[byCPU[i]] != cpuTotal[byCPU[j]] {
			return cpuTotal[byCPU[i]] > cpuTotal[byCPU[j]]
		}
		return byCPU[i] < byCPU[j]
	})
	byNet := append([]string(nil), byCPU...)
	sort.SliceStable(byNet, func(i, j int) bool { return netTotal[byNet[i]] > netTotal[byNet[j]] })
	named := map[string]bool{}
	for i, id := range byCPU {
		if i < usageTopTasks {
			named[id] = true
		}
	}
	for i, id := range byNet {
		if i < usageTopTasks && netTotal[id] > 0 {
			named[id] = true
		}
	}
	for _, id := range byCPU { // keep the biggest-CPU-first order
		if named[id] {
			resp.Tasks = append(resp.Tasks, id)
		}
	}

	byAt := map[int64]int{}
	for _, p := range points {
		pv := usagePointView{
			AtMS: p.At.UnixMilli(), Samples: p.Samples, HostCPUBusy: p.HostCPUBusy, HostMemUsed: p.HostMemUsed,
			DiskUsed: p.DiskUsed, TasksCPU: p.TasksCPU, TasksMemory: p.TasksMem, PerTask: map[string]usageShare{},
		}
		byAt[pv.AtMS] = len(resp.Series)
		resp.Series = append(resp.Series, pv)
	}
	for _, tp := range taskPoints {
		i, ok := byAt[tp.At.UnixMilli()]
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
		cur.TunnelOutBytes += tp.TunnelOut
		cur.TunnelInBytes += tp.TunnelIn
		resp.Series[i].PerTask[key] = cur
		resp.Series[i].TunnelOutBytes += tp.TunnelOut
		resp.Series[i].TunnelInBytes += tp.TunnelIn
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

type taskUsageLimits struct {
	Cores    float64 `json:"cores"`
	MemoryMB int     `json:"memory_mb"`
}

type taskUsagePoint struct {
	AtMS           int64   `json:"at_ms"`
	CPUCores       float64 `json:"cpu_cores"`
	MemoryBytes    int64   `json:"memory_bytes"`
	TunnelOutBytes int64   `json:"tunnel_out_bytes"`
	TunnelInBytes  int64   `json:"tunnel_in_bytes"`
}

type taskUsageResponse struct {
	// Supported is false when the machine's agent never reported this task (an agent built
	// before usage reporting, or a task that never started).
	Supported bool `json:"supported"`
	// AgentReportsUsage is whether the machine the task ran on has ever reported usage at all.
	// When it hasn't, no reading will ever come for this task: its agent is an older version.
	AgentReportsUsage bool             `json:"agent_reports_usage"`
	Limits            taskUsageLimits  `json:"limits"`
	Points            []taskUsagePoint `json:"points"`
	Summary           *taskUsageView   `json:"summary,omitempty"`
}

// maxTaskUsagePoints bounds one response: about 21 hours at the agent's 15 s readings.
// A longer task shows its most recent stretch.
const maxTaskUsagePoints = 5000

// handleTaskUsage is GET /api/portal/customer/tasks/{id}/usage: what the customer's own task
// actually used on the machine it ran on, against the limits it asked for. As reported by the
// machine's agent, so it is for information and never feeds billing.
func (s *Server) handleTaskUsage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	task, err := s.getOwnTask(r.Context(), sess.AccountID, r.PathValue("id"))
	if err != nil {
		writeStoreOrRPCError(w, err)
		return
	}
	readings, err := s.Store.TaskUsageSeries(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading usage")
		return
	}
	if len(readings) > maxTaskUsagePoints {
		readings = readings[len(readings)-maxTaskUsagePoints:]
	}
	resp := taskUsageResponse{
		Supported: len(readings) > 0,
		Limits:    taskUsageLimits{Cores: task.Limits.CPUCores, MemoryMB: task.Limits.MemoryMB},
		Points:    make([]taskUsagePoint, 0, len(readings)),
	}
	for _, rd := range readings {
		resp.Points = append(resp.Points, taskUsagePoint{
			AtMS: rd.At.UnixMilli(), CPUCores: rd.CPUCores, MemoryBytes: rd.MemoryBytes,
			TunnelOutBytes: rd.TunnelOut, TunnelInBytes: rd.TunnelIn,
		})
	}
	resp.AgentReportsUsage = resp.Supported
	if !resp.Supported && task.NodeID != nil {
		if _, ok, err := s.Store.NodeUsageLatest(r.Context(), *task.NodeID); err == nil {
			resp.AgentReportsUsage = ok
		}
	}
	if sums, err := s.Store.TaskUsageSummaries(r.Context(), []string{task.ID}); err == nil {
		if u, ok := sums[task.ID]; ok {
			resp.Summary = &taskUsageView{CoreSeconds: u.CoreSeconds, PeakMemoryBytes: u.PeakMemoryBytes,
				TunnelToGateway: u.TunnelToGateway, TunnelToTask: u.TunnelToTask}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
