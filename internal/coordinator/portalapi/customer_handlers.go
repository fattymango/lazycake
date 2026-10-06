package portalapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/id"
)

const recentTasksLimit = 200

// meResponse is GET /api/portal/customer/me's shape: balance and available
// balance (PLAN.md §3's "dashboard (balance + recent tasks)").
type meResponse struct {
	AccountID              string `json:"account_id"`
	Username               string `json:"username"`
	Role                   string `json:"role"`
	BalanceMicros          int64  `json:"balance_micros"`
	AvailableBalanceMicros int64  `json:"available_balance_micros"`
}

func (s *Server) handleCustomerMe(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	acct, err := s.Store.GetAccount(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading account")
		return
	}
	available, err := s.Store.AvailableBalance(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading balance")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		AccountID: sess.AccountID, Username: acct.Name, Role: string(sess.Role),
		BalanceMicros: acct.BalanceMicros, AvailableBalanceMicros: available,
	})
}

// taskView is the frontend's flat, snake_case task shape (web/src/shared/types.ts),
// the same convention internal/coordinator/dashboard's taskView already
// uses for the same reason: the wire format doesn't shift underfoot every
// time an unrelated store.Task field changes.
type taskView struct {
	ID           string            `json:"id"`
	State        string            `json:"state"`
	Image        string            `json:"image"`
	Entrypoint   []string          `json:"entrypoint,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	NodeID       string            `json:"node_id,omitempty"`
	Cores        float64           `json:"cores"`
	MemoryMB     int               `json:"memory_mb"`
	DiskMB       int               `json:"disk_mb,omitempty"`
	ExitCode     *int              `json:"exit_code,omitempty"`
	ExitReason   string            `json:"exit_reason,omitempty"`
	CreatedAtMS  int64             `json:"created_at_ms"`
	StartedAtMS  int64             `json:"started_at_ms,omitempty"`
	FinishedAtMS int64             `json:"finished_at_ms,omitempty"`
}

func toTaskView(t store.Task) taskView {
	tv := taskView{
		ID: t.ID, State: string(t.State), Image: t.Image,
		Entrypoint: t.Entrypoint, Args: t.Args, Env: t.Env,
		Cores: t.Limits.CPUCores, MemoryMB: t.Limits.MemoryMB, DiskMB: t.Limits.DiskMB,
		ExitCode: t.ExitCode, CreatedAtMS: t.CreatedAt.UnixMilli(),
	}
	if t.NodeID != nil {
		tv.NodeID = *t.NodeID
	}
	if t.ExitReason != nil {
		tv.ExitReason = *t.ExitReason
	}
	if t.StartedAt != nil {
		tv.StartedAtMS = t.StartedAt.UnixMilli()
	}
	if t.FinishedAt != nil {
		tv.FinishedAtMS = t.FinishedAt.UnixMilli()
	}
	return tv
}

// submitTaskRequest is POST /api/portal/customer/tasks' body, matching
// web/src/shared/types.ts's SubmitTaskRequest.
type submitTaskRequest struct {
	Image         string            `json:"image"`
	Entrypoint    []string          `json:"entrypoint"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env"`
	Workdir       string            `json:"workdir"`
	Cores         float64           `json:"cores"`
	MemoryMB      int               `json:"memory_mb"`
	DiskMB        int               `json:"disk_mb"`
	WallTimeoutS  int               `json:"wall_timeout_s"`
	TunnelTargets []struct {
		GatewayID string `json:"gateway_id"`
		Hostname  string `json:"hostname"`
		Port      int32  `json:"port"`
	} `json:"tunnel_targets"`
	IdempotencyKey string `json:"idempotency_key"`
}

// handleSubmitTask implements task 7.4's POST /api/portal/customer/tasks -
// deferring every real check (digest pin, gateway ownership, balance) to
// api.CustomerServer.SubmitTaskForAccount, the same logic the gRPC
// SubmitTask RPC uses.
func (s *Server) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	var req submitTaskRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	wallTimeout := req.WallTimeoutS
	if wallTimeout <= 0 {
		wallTimeout = 300 // a sane portal default; lcctl callers must set it explicitly, but a web form shouldn't have to expose it for the common case
	}
	targets := make([]api.TunnelTargetSpec, len(req.TunnelTargets))
	for i, t := range req.TunnelTargets {
		targets[i] = api.TunnelTargetSpec{GatewayID: t.GatewayID, Hostname: t.Hostname, Port: t.Port}
	}

	task, err := s.Customer.SubmitTaskForAccount(r.Context(), sess.AccountID, api.SubmitTaskParams{
		Image: req.Image, Entrypoint: req.Entrypoint, Args: req.Args, Env: req.Env, Workdir: req.Workdir,
		Limits: store.Limits{
			CPUCores: req.Cores, MemoryMB: req.MemoryMB, DiskMB: req.DiskMB, WallTimeoutS: wallTimeout,
		},
		Targets: targets, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeStoreOrRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toTaskView(task))
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	tasks, err := s.Store.ListTasksByAccount(r.Context(), sess.AccountID, recentTasksLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing tasks")
		return
	}
	out := make([]taskView, len(tasks))
	for i, t := range tasks {
		out[i] = toTaskView(t)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	task, err := s.getOwnTask(r.Context(), sess.AccountID, r.PathValue("id"))
	if err != nil {
		writeStoreOrRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTaskView(task))
}

// getOwnTask fetches a task and 404s (never a distinguishing error) if it
// doesn't exist or belongs to a different account - the same "don't reveal
// another account's resources" posture api.CustomerServer.GetTask already
// uses.
func (s *Server) getOwnTask(ctx context.Context, accountID, taskID string) (store.Task, error) {
	task, err := s.Store.GetTask(ctx, taskID)
	if err != nil || task.AccountID != accountID {
		return store.Task{}, notFoundErr("task not found")
	}
	return task, nil
}

// handleTaskLogs implements task 7.4's GET .../tasks/{id}/logs as SSE,
// "same polling shape StreamLogs already uses server-side"
// (api.CustomerServer.StreamLogs): poll ListLogs on an interval, stop once
// the task is terminal and there's nothing left to send.
//
// Every frame carries id:<seq> and a reconnect's Last-Event-ID header is
// honoured, so a client that drops and resumes never sees a line twice.
// When the task is finished and drained the stream ends with an explicit
// "end" event: without it the browser's EventSource reads the close as a
// dropped connection, reconnects, and the whole log replays from the start
// - forever, for a failed task.
func (s *Server) handleTaskLogs(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	taskID := r.PathValue("id")
	if _, err := s.getOwnTask(r.Context(), sess.AccountID, taskID); err != nil {
		writeStoreOrRPCError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	var sinceSeq int64
	if last := r.Header.Get("Last-Event-ID"); last != "" {
		if n, err := strconv.ParseInt(last, 10, 64); err == nil && n > 0 {
			sinceSeq = n
		}
	}
	for {
		lines, err := s.Store.ListLogs(ctx, taskID, sinceSeq)
		if err != nil {
			return
		}
		for _, l := range lines {
			writeSSEJSONWithID(w, l.Seq, logLineView{Seq: l.Seq, Stream: l.Stream, AtMS: l.At.UnixMilli(), Line: l.Line})
			sinceSeq = l.Seq
		}
		flusher.Flush()

		task, err := s.Store.GetTask(ctx, taskID)
		if err != nil {
			return
		}
		if isTerminal(task.State) && len(lines) == 0 {
			writeSSEEnd(w)
			flusher.Flush()
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

type logLineView struct {
	Seq    int64  `json:"seq"`
	Stream string `json:"stream"`
	AtMS   int64  `json:"at_ms"`
	Line   string `json:"line"`
}

func isTerminal(st store.TaskState) bool {
	switch st {
	case store.TaskSucceeded, store.TaskFailed, store.TaskFenced, store.TaskAbandoned, store.TaskCancelled:
		return true
	}
	return false
}

// gatewayView mirrors web/src/shared/types.ts's Gateway.
type gatewayView struct {
	ID          string               `json:"id"`
	Label       string               `json:"label"`
	Connected   bool                 `json:"connected"`
	Services    []gatewayServiceView `json:"services"`
	CreatedAtMS int64                `json:"created_at_ms"`
}

type gatewayServiceView struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

func toGatewayView(g store.Gateway) gatewayView {
	services := make([]gatewayServiceView, len(g.Services))
	for i, svc := range g.Services {
		services[i] = gatewayServiceView{Name: svc.Name, Port: svc.Port}
	}
	return gatewayView{ID: g.ID, Label: g.Label, Connected: g.Connected, Services: services, CreatedAtMS: g.CreatedAt.UnixMilli()}
}

func (s *Server) handleListGateways(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	gateways, err := s.Store.ListGatewaysByAccount(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing gateways")
		return
	}
	out := make([]gatewayView, len(gateways))
	for i, g := range gateways {
		out[i] = toGatewayView(g)
	}
	writeJSON(w, http.StatusOK, out)
}

type createGatewayRequest struct {
	Label    string               `json:"label"`
	Services []gatewayServiceView `json:"services"`
}

// createGatewayResponse is gatewayView plus the one-time install token -
// a separate type from gatewayView itself (rather than an optional field
// on it) so there's no risk of ever accidentally including a live token
// in a *list* response; toGatewayView/gatewayView are used there and
// nowhere near a token.
type createGatewayResponse struct {
	gatewayView
	InstallToken string `json:"install_token"`
}

// handleCreateGateway mirrors api.CustomerServer.CreateGateway (task 7.4:
// "this already exists as CustomerService RPCs; the portal is a UI on top,
// not new logic"), reimplemented directly against the store rather than
// through SubmitTaskForAccount's pattern since CreateGateway has no
// balance/ownership checks worth centralizing the same way.
//
// Minting the install token here (not just the Gateway row) was missing
// entirely until caught live: a gateway created through the portal had no
// way to ever actually run, since nothing authenticates a real gateway
// process without one - the gRPC CreateGateway this was meant to mirror
// always did this (internal/coordinator/api/customer_server.go), the
// portal version just never got the token-minting half copied over.
func (s *Server) handleCreateGateway(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	var req createGatewayRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Label == "" {
		writeError(w, http.StatusBadRequest, "label is required")
		return
	}
	services := make([]store.GatewayService, len(req.Services))
	for i, svc := range req.Services {
		if svc.Name == "" || svc.Port <= 0 {
			writeError(w, http.StatusBadRequest, "each service needs a name and a port")
			return
		}
		services[i] = store.GatewayService{Name: svc.Name, Port: svc.Port}
	}

	gatewayID := id.New(id.Gateway)
	if err := s.Store.CreateGateway(r.Context(), store.Gateway{
		ID: gatewayID, AccountID: sess.AccountID, Label: req.Label, Services: services,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "creating gateway")
		return
	}

	installToken, err := newSessionID() // 32 random bytes, hex-encoded - same shape as api.randomToken, no need for a second generator
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generating install token")
		return
	}
	if err := s.Store.CreateToken(r.Context(), store.APIToken{
		TokenHash: auth.Hash(installToken), AccountID: sess.AccountID, Kind: store.TokenGateway,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "generating install token")
		return
	}

	gw, err := s.Store.GetGateway(r.Context(), gatewayID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating gateway")
		return
	}
	writeJSON(w, http.StatusCreated, createGatewayResponse{gatewayView: toGatewayView(gw), InstallToken: installToken})
}

// ledgerView mirrors web/src/shared/types.ts's LedgerEntry.
type ledgerView struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	Kind         string `json:"kind"`
	AmountMicros int64  `json:"amount_micros"`
	CreatedAtMS  int64  `json:"created_at_ms"`
}

func toLedgerView(e store.LedgerEntry) ledgerView {
	return ledgerView{ID: e.ID, TaskID: e.TaskID, Kind: e.Kind, AmountMicros: e.AmountMicros, CreatedAtMS: e.CreatedAt.UnixMilli()}
}

func (s *Server) handleCustomerLedger(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	entries, err := s.Store.LedgerEntriesForAccount(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing ledger")
		return
	}
	out := make([]ledgerView, len(entries))
	for i, e := range entries {
		out[i] = toLedgerView(e)
	}
	writeJSON(w, http.StatusOK, out)
}

type addBalanceRequest struct {
	AmountMicros int64 `json:"amount_micros"`
}

// handleAddBalance implements task 7.4's dev-only POST
// /api/portal/customer/balance/add (PLAN.md §3: "a dev-only 'add funds'
// button, since there's no real payment rail and none is being added
// here"). It is not gated behind cfg.Dev at the handler level - see
// OPEN_QUESTIONS.md: whether this route should refuse to exist outside a
// dev deployment, or stay a permanent (if clearly labeled) faucet, is a
// deployment decision cmd/coordinator's wiring should make, not something
// to bake into this package.
func (s *Server) handleAddBalance(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	var req addBalanceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.AmountMicros <= 0 {
		writeError(w, http.StatusBadRequest, "amount_micros must be positive")
		return
	}
	balance, err := s.Store.AdjustBalance(r.Context(), sess.AccountID, req.AmountMicros)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "adding funds")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"balance_micros": balance})
}

// notFoundErr lets getOwnTask/getOwnNode hand writeStoreOrRPCError a status
// it already knows how to map (codes.NotFound -> 404), without
// api.CustomerServer's exact message wording ("task not found") having to
// live in two packages.
func notFoundErr(msg string) error {
	return status.Error(codes.NotFound, msg)
}
