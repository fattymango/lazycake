package portalapi

import (
	"context"
	"net/http"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

const providerNodeTasksLimit = 100

// providerMeResponse is GET /api/portal/provider/me's shape: lifetime
// earnings (PLAN.md §3's "dashboard (connected machines + lifetime
// earnings)") - the sum of every credit ledger entry ever recorded for
// this account, task 7.5's own wording.
type providerMeResponse struct {
	AccountID              string `json:"account_id"`
	Username               string `json:"username"`
	Role                   string `json:"role"`
	LifetimeEarningsMicros int64  `json:"lifetime_earnings_micros"`
}

func (s *Server) handleProviderMe(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	acct, err := s.Store.GetAccount(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading account")
		return
	}
	earnings, err := s.lifetimeEarnings(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading earnings")
		return
	}
	writeJSON(w, http.StatusOK, providerMeResponse{
		AccountID: sess.AccountID, Username: acct.Name, Role: string(sess.Role),
		LifetimeEarningsMicros: earnings,
	})
}

// lifetimeEarnings sums only the credit rows (a provider's own ledger also
// holds nothing else, since it never submits tasks as a customer through
// the same account - but summing kind='credit' explicitly documents that
// intent rather than relying on it).
func (s *Server) lifetimeEarnings(ctx context.Context, accountID string) (int64, error) {
	entries, err := s.Store.LedgerEntriesForAccount(ctx, accountID)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		if e.Kind == "credit" {
			total += e.AmountMicros
		}
	}
	return total, nil
}

// nodeView mirrors web/src/shared/types.ts's Node.
type nodeView struct {
	ID                string  `json:"id"`
	Hostname          string  `json:"hostname"`
	Arch              string  `json:"arch"`
	Connected         bool    `json:"connected"`
	OfferCores        float64 `json:"offer_cores"`
	OfferMemoryMB     int     `json:"offer_memory_mb"`
	OfferDiskMB       int     `json:"offer_disk_mb"`
	TrustScore        float64 `json:"trust_score"`
	LastHeartbeatAtMS int64   `json:"last_heartbeat_at_ms,omitempty"`
	CreatedAtMS       int64   `json:"created_at_ms"`
}

func toNodeView(n store.Node) nodeView {
	nv := nodeView{
		ID: n.ID, Hostname: n.Hostname, Arch: n.Arch, Connected: n.Connected,
		OfferCores: n.OfferCores, OfferMemoryMB: n.OfferMemoryMB, OfferDiskMB: n.OfferDiskMB,
		TrustScore: n.TrustScore, CreatedAtMS: n.CreatedAt.UnixMilli(),
	}
	if n.LastHeartbeatAt != nil {
		nv.LastHeartbeatAtMS = n.LastHeartbeatAt.UnixMilli()
	}
	return nv
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	nodes, err := s.Store.ListNodesByAccount(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing machines")
		return
	}
	out := make([]nodeView, len(nodes))
	for i, n := range nodes {
		out[i] = toNodeView(n)
	}
	writeJSON(w, http.StatusOK, out)
}

// getOwnNode fetches a node and 404s (never a distinguishing error) if it
// doesn't exist or belongs to a different account.
func (s *Server) getOwnNode(ctx context.Context, accountID, nodeID string) (store.Node, error) {
	node, err := s.Store.GetNode(ctx, nodeID)
	if err != nil || node.AccountID != accountID {
		return store.Node{}, notFoundErr("machine not found")
	}
	return node, nil
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	node, err := s.getOwnNode(r.Context(), sess.AccountID, r.PathValue("id"))
	if err != nil {
		writeStoreOrRPCError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNodeView(node))
}

// handleNodeTasks implements GET /api/portal/provider/nodes/{id}/tasks -
// not in IMPLEMENTATION.md's Appendix table (see
// docs/01-dashboard-portals/OPEN_QUESTIONS.md's "invented, not in the
// plan's endpoint table" note from the frontend build): task 7.5 calls for
// machine detail to show "recent tasks" via the already-existing
// ListTasksByNode, but the plan's own endpoint list only has
// GET /nodes[/{id}]. Kept as its own route rather than folded into
// GET /nodes/{id} so a machine with a long task history doesn't inflate
// every node-detail response the same way.
func (s *Server) handleNodeTasks(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	nodeID := r.PathValue("id")
	if _, err := s.getOwnNode(r.Context(), sess.AccountID, nodeID); err != nil {
		writeStoreOrRPCError(w, err)
		return
	}
	tasks, err := s.Store.ListTasksByNode(r.Context(), nodeID, []store.TaskState{
		store.TaskQueued, store.TaskReserved, store.TaskDispatched, store.TaskRunning,
		store.TaskSucceeded, store.TaskFailed, store.TaskFenced, store.TaskAbandoned, store.TaskCancelled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing machine tasks")
		return
	}
	if len(tasks) > providerNodeTasksLimit {
		tasks = tasks[:providerNodeTasksLimit]
	}
	out := make([]taskView, len(tasks))
	for i, t := range tasks {
		out[i] = toTaskView(t)
	}
	writeJSON(w, http.StatusOK, out)
}

type installTokenResponse struct {
	Token          string `json:"token"`
	InstallCommand string `json:"install_command"`
}

// handleInstallToken implements task 7.5's POST
// /api/portal/provider/nodes/install-token: mints a fresh agent-kind API
// token, shown once, same contract as CreateGateway's install_token.
func (s *Server) handleInstallToken(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	token, err := newSessionID() // 32 random bytes, hex-encoded - same shape as api.randomToken, no need for a second generator
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generating install token")
		return
	}
	if err := s.Store.CreateToken(r.Context(), store.APIToken{
		TokenHash: auth.Hash(token), AccountID: sess.AccountID, Kind: store.TokenAgent,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "generating install token")
		return
	}
	writeJSON(w, http.StatusCreated, installTokenResponse{
		Token:          token,
		InstallCommand: s.installCommand(token),
	})
}

// installCommand builds a real, runnable podman command against this
// repo's own agent image (deploy/Dockerfile's "agent" target) and the
// agent's actual LAZYCAKE_* env vars (internal/agent/config), the same
// shape deploy/docker-compose.yml's agent1 service uses. It assumes the
// image was already built locally (`podman build --target agent -f
// deploy/Dockerfile -t lazycake-agent .`) - a real one-line "docker pull
// and run" story needs a published image registry, which is a deployment
// decision this code has no business making up (see OPEN_QUESTIONS.md).
func (s *Server) installCommand(token string) string {
	coordinatorAddr := s.CoordinatorAddr
	if coordinatorAddr == "" {
		coordinatorAddr = "<coordinator-host>:7443"
	}
	return "podman run -d --name lazycake-agent" +
		" -e LAZYCAKE_COORDINATOR_ADDR=" + coordinatorAddr +
		" -e LAZYCAKE_TOKEN=" + token +
		" -e LAZYCAKE_OFFER_CORES=<cores> -e LAZYCAKE_OFFER_MEMORY_MB=<memory_mb> -e LAZYCAKE_OFFER_DISK_MB=<disk_mb>" +
		" -v /run/podman/podman.sock:/run/lazycake-engine/podman/podman.sock" +
		" -e XDG_RUNTIME_DIR=/run/lazycake-engine -e CONTAINER_HOST=unix:///run/lazycake-engine/podman/podman.sock" +
		" lazycake-agent"
}

func (s *Server) handleProviderLedger(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	entries, err := s.Store.LedgerEntriesForAccount(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing earnings")
		return
	}
	out := make([]ledgerView, len(entries))
	for i, e := range entries {
		out[i] = toLedgerView(e)
	}
	writeJSON(w, http.StatusOK, out)
}
