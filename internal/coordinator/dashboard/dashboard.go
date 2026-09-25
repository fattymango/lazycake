// Package dashboard implements IMPLEMENTATION.md tasks 6.1 and 6.2: a
// server-sent-events stream of coordinator activity, and a single
// self-contained HTML page (no build step, no npm) that renders it live.
// It depends on store.Store, events.Bus and scheduler.TrustTracker only
// through what it actually reads from them, so it never has an opinion
// about how the rest of the coordinator is wired together.
package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/scheduler"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

//go:embed index.html
var indexHTML embed.FS

// Server serves the dashboard page, its initial-state snapshot, and the
// SSE stream that keeps it live afterward.
type Server struct {
	Store store.Store
	Bus   *events.Bus
	Trust *scheduler.TrustTracker
	Log   *slog.Logger
}

// Handler returns the mux to serve on the coordinator's HTTP address.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/events", s.handleEvents)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, err := indexHTML.ReadFile("index.html")
	if err != nil {
		http.Error(w, "dashboard unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(page)
}

// nodeView/taskView are the dashboard's own flat JSON shapes - deliberately
// not store.Node/store.Task themselves, so the wire format doesn't shift
// underfoot every time an unrelated field is added to either.
type nodeView struct {
	ID            string  `json:"id"`
	Hostname      string  `json:"hostname"`
	Connected     bool    `json:"connected"`
	OfferCores    float64 `json:"offer_cores"`
	OfferMemoryMB int     `json:"offer_memory_mb"`
	OfferDiskMB   int     `json:"offer_disk_mb"`
	TrustScore    float64 `json:"trust_score"`
}

type taskView struct {
	ID         string  `json:"id"`
	State      string  `json:"state"`
	NodeID     string  `json:"node_id,omitempty"`
	Image      string  `json:"image"`
	ExitReason string  `json:"exit_reason,omitempty"`
	CreatedAt  int64   `json:"created_at_ms"`
	Cores      float64 `json:"cores"`
	MemoryMB   int     `json:"memory_mb"`
}

type stateResponse struct {
	Nodes  []nodeView `json:"nodes"`
	Tasks  []taskView `json:"tasks"`
	Totals struct {
		ChargesMicros int64 `json:"charges_micros"`
		CreditsMicros int64 `json:"credits_micros"`
	} `json:"totals"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	nodes, err := s.Store.ListNodes(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("listing nodes: %v", err), http.StatusInternalServerError)
		return
	}
	tasks, err := s.Store.ListRecentTasks(ctx, 100)
	if err != nil {
		http.Error(w, fmt.Sprintf("listing tasks: %v", err), http.StatusInternalServerError)
		return
	}
	charges, credits, err := s.Store.LedgerTotals(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("summing ledger: %v", err), http.StatusInternalServerError)
		return
	}

	resp := stateResponse{}
	for _, n := range nodes {
		trust := 0.5
		if s.Trust != nil {
			trust = s.Trust.Score(n.ID)
		}
		resp.Nodes = append(resp.Nodes, nodeView{
			ID: n.ID, Hostname: n.Hostname, Connected: n.Connected,
			OfferCores: n.OfferCores, OfferMemoryMB: n.OfferMemoryMB, OfferDiskMB: n.OfferDiskMB,
			TrustScore: trust,
		})
	}
	for _, t := range tasks {
		tv := taskView{
			ID: t.ID, State: string(t.State), Image: t.Image,
			CreatedAt: t.CreatedAt.UnixMilli(), Cores: t.Limits.CPUCores, MemoryMB: t.Limits.MemoryMB,
		}
		if t.NodeID != nil {
			tv.NodeID = *t.NodeID
		}
		if t.ExitReason != nil {
			tv.ExitReason = *t.ExitReason
		}
		resp.Tasks = append(resp.Tasks, tv)
	}
	resp.Totals.ChargesMicros = charges
	resp.Totals.CreditsMicros = credits

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.Log.Warn("encoding dashboard state", "error", err)
	}
}

// handleEvents implements task 6.1: GET /events as server-sent events,
// one JSON-encoded events.Event per "data:" line.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, cancel := s.Bus.Subscribe()
	defer cancel()

	// A comment line keeps the connection alive through idle proxies and
	// gives curl -N something to show immediately rather than sitting
	// silent until the first real event.
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
