package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/scheduler"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// fakeDashboardStore is a minimal store.Store double covering just what
// dashboard.Server reads.
type fakeDashboardStore struct {
	store.Store
	nodes []store.Node
	tasks []store.Task
}

func (f *fakeDashboardStore) ListNodes(ctx context.Context) ([]store.Node, error) {
	return f.nodes, nil
}
func (f *fakeDashboardStore) ListRecentTasks(ctx context.Context, limit int) ([]store.Task, error) {
	return f.tasks, nil
}
func (f *fakeDashboardStore) LedgerTotals(ctx context.Context) (int64, int64, error) {
	return 1_500_000, 1_200_000, nil
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestSSEStream is task 6.1's own verify: GET /events streams JSON events
// as things happen, proven with a real HTTP round trip against
// httptest.Server rather than just unit-testing events.Bus in isolation.
func TestSSEStream(t *testing.T) {
	bus := events.NewBus()
	srv := &Server{Store: &fakeDashboardStore{}, Bus: bus, Trust: scheduler.NewTrustTracker(), Log: discardLog()}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/events", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	reader := bufio.NewReader(resp.Body)

	// Give the handler a moment to subscribe before publishing, so the
	// event isn't published before anyone's listening.
	time.Sleep(50 * time.Millisecond)
	bus.Publish(events.Event{Type: "task_state", AtMS: 123, TaskID: "tsk_1", State: "running"})

	dataLine := ""
	deadline := time.After(5 * time.Second)
	lineCh := make(chan string, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "data: ") {
				lineCh <- line
				return
			}
		}
	}()
	select {
	case dataLine = <-lineCh:
	case <-deadline:
		t.Fatal("never received an SSE data line")
	}

	payload := strings.TrimPrefix(strings.TrimSpace(dataLine), "data: ")
	var ev events.Event
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		t.Fatalf("unmarshalling event JSON %q: %v", payload, err)
	}
	if ev.Type != "task_state" || ev.TaskID != "tsk_1" || ev.State != "running" {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

// TestStateEndpoint proves GET /api/state (the dashboard's initial
// snapshot, task 6.2) returns the node/task/ledger data it claims to.
func TestStateEndpoint(t *testing.T) {
	fs := &fakeDashboardStore{
		nodes: []store.Node{{ID: "nod_1", Hostname: "h1", Connected: true, OfferCores: 4}},
		tasks: []store.Task{{ID: "tsk_1", State: store.TaskRunning, Image: "alpine@sha256:x"}},
	}
	trust := scheduler.NewTrustTracker()
	trust.CleanCompletion("nod_1")
	srv := &Server{Store: fs, Bus: events.NewBus(), Trust: trust, Log: discardLog()}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("GET /api/state: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got stateResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].ID != "nod_1" || got.Nodes[0].TrustScore <= 0.5 {
		t.Fatalf("unexpected nodes: %+v", got.Nodes)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].ID != "tsk_1" || got.Tasks[0].State != "running" {
		t.Fatalf("unexpected tasks: %+v", got.Tasks)
	}
	if got.Totals.ChargesMicros != 1_500_000 || got.Totals.CreditsMicros != 1_200_000 {
		t.Fatalf("unexpected totals: %+v", got.Totals)
	}
}

// TestIndexServesHTML confirms the dashboard page itself is served at "/".
func TestIndexServesHTML(t *testing.T) {
	srv := &Server{Store: &fakeDashboardStore{}, Bus: events.NewBus(), Trust: scheduler.NewTrustTracker(), Log: discardLog()}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "LazyCake") || !strings.Contains(string(body), "EventSource") {
		t.Fatalf("index page missing expected content")
	}
}
