package portalapi

import (
	"context"
	"net/http"

	"github.com/mkassab215/lazycake/internal/coordinator/auth"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
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

// handleDeleteNode implements DELETE /api/portal/provider/nodes/{id}: the
// machine-management gap this session's live testing surfaced - there was
// no way to remove a machine from the fleet at all, active or not. If the
// node is still connected, this first asks its agent to exit cleanly (a
// Shutdown message - best-effort, see the proto doc comment: nothing can
// force a process to exit on hardware the coordinator doesn't own), then
// deletes the node row either way. A node that's still genuinely running
// and ignores Shutdown will just reappear on its own next heartbeat/
// register cycle - deleting the DB row doesn't revoke its ability to
// reconnect, only removes today's stale entry.
func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	nodeID := r.PathValue("id")
	if _, err := s.getOwnNode(r.Context(), sess.AccountID, nodeID); err != nil {
		writeStoreOrRPCError(w, err)
		return
	}

	if s.Registry != nil {
		msg := &lazycakev1.CoordinatorMessage{Body: &lazycakev1.CoordinatorMessage_Shutdown{
			Shutdown: &lazycakev1.Shutdown{Reason: "removed from provider portal"},
		}}
		if err := s.Registry.Send(nodeID, msg); err != nil {
			s.Log.Info("machine not connected, deleting without a shutdown request", "node_id", nodeID, "error", err)
		}
	}

	if err := s.Store.DeleteNode(r.Context(), nodeID); err != nil {
		writeStoreOrRPCError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	summaries, err := s.Store.TaskUsageSummaries(r.Context(), ids)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading task usage")
		return
	}
	out := make([]nodeTaskView, len(tasks))
	for i, t := range tasks {
		out[i] = nodeTaskView{taskView: toTaskView(t)}
		if u, ok := summaries[t.ID]; ok {
			out[i].Usage = &taskUsageView{CoreSeconds: u.CoreSeconds, PeakMemoryBytes: u.PeakMemoryBytes,
				TunnelToGateway: u.TunnelToGateway, TunnelToTask: u.TunnelToTask}
		}
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

// defaultOfferCores/MemoryMB/DiskMB match deploy/docker-compose.yml's own
// agent1 example values - real numbers, not placeholders, so the command
// installCommand returns is actually runnable as-is (a bare "<cores>" is
// valid JSON but not valid shell: bash parses "<" as redirection, so a
// literal placeholder there breaks copy-paste instead of just being a
// no-op reminder to edit it). The doc comment below is where "adjust
// these" belongs, not the command itself.
const (
	defaultOfferCores    = "2"
	defaultOfferMemoryMB = "2048"
	defaultOfferDiskMB   = "8192"
)

// installCommand builds a real, runnable, copy-pasteable podman command
// against this repo's own agent image (deploy/Dockerfile's "agent"
// target) and the agent's actual LAZYCAKE_* env vars (internal/agent/
// config), the same shape deploy/docker-compose.yml's agent1 service
// uses - offer_cores/memory/disk are real defaults to edit after pasting,
// not placeholders to fill in before it'll run. Assumes the image was
// already built locally (`podman build --target agent -f deploy/Dockerfile
// -t lazycake-agent .`) - a real one-line "docker pull and run" story
// needs a published image registry, which is a deployment decision this
// code has no business making up (see OPEN_QUESTIONS.md).
//
// --replace: a fixed container name means minting a second token and
// re-running the command (retrying after a mistake, or just trying it
// again) collides with whatever "lazycake-agent" already exists instead
// of just working - hit live, more than once. --replace makes re-running
// this command idempotent instead of requiring a manual `podman rm -f`
// first.
//
// The podman.sock bind mount defaults to the *rootless* path,
// /run/user/$(id -u)/podman/podman.sock - "$(id -u)" is a real shell
// command substitution, evaluated by whatever shell actually runs this
// command, not a placeholder the person pasting it has to notice and
// edit first (that was tried first: a same-line "# rootless podman: use
// ... instead" comment. Two different people copy-pasted the command
// with the rootful path left in anyway and got a silent, confusing
// "permission denied" deep in the agent's own capability-probe logs, no
// error at the podman-run step itself to catch it - a trailing comment
// that's easy to not read is the wrong fix when the command can just be
// correct by default instead). Rootless is also genuinely the common
// case for "lend spare capacity from a machine you personally use,"
// this product's actual target - a rootful engine is the one that now
// gets the fallback comment.
// agentImage is the public image the install command pulls. A bare
// "lazycake-agent" resolves against docker.io/library and is denied, so the
// full name is spelled out.
const agentImage = "docker.io/fattymango/lazycake-agent:latest"

// lcinitHostDir is expanded by the shell that runs the install command, so
// it resolves to the provider's own home directory.
const lcinitHostDir = "$HOME/.local/share/lazycake/bin"

func (s *Server) installCommand(token string) string {
	coordinatorAddr := s.CoordinatorAddr
	if coordinatorAddr == "" {
		coordinatorAddr = "COORDINATOR_HOST:7443"
	}
	return "mkdir -p " + lcinitHostDir + " && podman run -d --replace --name lazycake-agent" +
		// --pid=host + --cap-add=SYS_ADMIN: this agent runs as a sibling
		// container talking to the host's own podman engine (the -v
		// socket mount below), the same as every task container it will
		// later spawn - so nsenter'ing into one of those siblings for a
		// task's tunnel_targets (internal/agent/netns/proxy.go) needs to
		// see its host PID (--pid=host) and hold the capability to join
		// its network namespace (--cap-add=SYS_ADMIN), since the agent
		// and its task containers already share one rootless user
		// namespace and can't gain more by joining it again. Without
		// these, any task with tunnel_targets fails outright - caught
		// live installing an agent by hand from this exact command.
		" --pid=host --cap-add=SYS_ADMIN" +
		" -e LAZYCAKE_COORDINATOR_ADDR=" + coordinatorAddr +
		" -e LAZYCAKE_TOKEN=" + token +
		" -e LAZYCAKE_OFFER_CORES=" + defaultOfferCores +
		" -e LAZYCAKE_OFFER_MEMORY_MB=" + defaultOfferMemoryMB +
		" -e LAZYCAKE_OFFER_DISK_MB=" + defaultOfferDiskMB +
		// The agent copies its bundled lcinit into this directory and
		// mounts that copy into tasks. The same path is mounted on both
		// sides because the engine resolves bind sources on the host,
		// where a path inside the agent's own image doesn't exist - that
		// mismatch made every task fail with "mkdir /usr/local/bin/lcinit:
		// permission denied".
		" -v " + lcinitHostDir + ":" + lcinitHostDir + " -e LAZYCAKE_LCINIT_HOST_DIR=" + lcinitHostDir +
		` -v /run/user/$(id -u)/podman/podman.sock:/run/lazycake-engine/podman/podman.sock` +
		" -e XDG_RUNTIME_DIR=/run/lazycake-engine -e CONTAINER_HOST=unix:///run/lazycake-engine/podman/podman.sock" +
		" " + agentImage +
		" # rootful podman instead? use /run/podman/podman.sock in the -v flag above"
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
