package portalapi

import (
	"log/slog"
	"net/http"

	"github.com/mkassab215/lazycake/internal/coordinator/api"
	"github.com/mkassab215/lazycake/internal/coordinator/events"
	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

// Server implements every /api/portal/* handler (IMPLEMENTATION.md tasks
// 7.2-7.5). It wraps api.CustomerServer for task submission (task 7.4:
// "don't duplicate the digest-pin/gateway-ownership/balance checks") and
// store.Store directly for everything account-scoped enough that no
// existing gRPC method already does it.
type Server struct {
	Store store.Store
	// Customer reuses api.CustomerServer.SubmitTaskForAccount so task
	// submission validation exists in exactly one place regardless of
	// transport. Required for the customer routes; the provider-only
	// routes never touch it.
	Customer *api.CustomerServer
	// Registry lets the "stop this machine" action ask a still-connected
	// node's agent process to exit (a best-effort Shutdown message - see
	// proto/lazycake/v1/agent.proto's Shutdown doc comment for why this
	// can only ever be a request, not a guarantee). Required for the
	// provider node-delete route; nil is only valid in tests that never
	// exercise it.
	Registry *api.Registry
	Bus      *events.Bus
	Log      *slog.Logger
	// Dev mirrors config.Config.Dev: the session cookie is Secure unless
	// this is true (PLAN.md §2 - local/demo compose serves plain HTTP).
	Dev bool
	// CoordinatorAddr is the address an agent should be told to dial,
	// used only to build the install command task 7.5's "add a machine"
	// page shows (host:port an operator's machine can actually reach -
	// not necessarily cfg.GRPCAddr's bind address verbatim behind NAT or
	// a reverse proxy; see OPEN_QUESTIONS.md).
	CoordinatorAddr string

	limiter *loginLimiter
}

// Handler returns the mux to mount at /api/portal/ on the coordinator's
// existing HTTP server, alongside the operator dashboard
// (cmd/coordinator/run.go). Every path here already starts with
// /api/portal/, so mounting it does not touch any of the dashboard's own
// routes; relocating the dashboard itself to /ops and serving the built
// portal frontends is task 7.10, still open (see
// docs/01-dashboard-portals/PROGRESS.md).
func (s *Server) Handler() http.Handler {
	if s.limiter == nil {
		s.limiter = newLoginLimiter()
	}

	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/portal/customer/signup", s.handleSignup(store.RoleCustomer))
	mux.HandleFunc("POST /api/portal/provider/signup", s.handleSignup(store.RoleProvider))
	mux.HandleFunc("POST /api/portal/customer/login", s.handleLogin(store.RoleCustomer))
	mux.HandleFunc("POST /api/portal/provider/login", s.handleLogin(store.RoleProvider))
	// Unified: an account's role is fixed at signup, so a login doesn't
	// need to already know which portal it's for - one login form for
	// both. The role-specific /customer/login and /provider/login above
	// stay too (harmless, and simpler for a caller that already knows).
	mux.HandleFunc("POST /api/portal/login", s.handleUnifiedLogin())
	mux.HandleFunc("GET /api/portal/me", s.requireAnySession(s.handleWhoami))
	mux.HandleFunc("POST /api/portal/logout", s.handleLogout)

	mux.HandleFunc("GET /api/portal/customer/me", s.requireSession(store.RoleCustomer, s.handleCustomerMe))
	mux.HandleFunc("POST /api/portal/customer/tasks", s.requireSession(store.RoleCustomer, s.handleSubmitTask))
	mux.HandleFunc("GET /api/portal/customer/tasks", s.requireSession(store.RoleCustomer, s.handleListTasks))
	mux.HandleFunc("GET /api/portal/customer/tasks/{id}", s.requireSession(store.RoleCustomer, s.handleGetTask))
	mux.HandleFunc("GET /api/portal/customer/tasks/{id}/logs", s.requireSession(store.RoleCustomer, s.handleTaskLogs))
	mux.HandleFunc("GET /api/portal/customer/gateways", s.requireSession(store.RoleCustomer, s.handleListGateways))
	mux.HandleFunc("POST /api/portal/customer/gateways", s.requireSession(store.RoleCustomer, s.handleCreateGateway))
	mux.HandleFunc("GET /api/portal/customer/ledger", s.requireSession(store.RoleCustomer, s.handleCustomerLedger))
	mux.HandleFunc("POST /api/portal/customer/balance/add", s.requireSession(store.RoleCustomer, s.handleAddBalance))
	mux.HandleFunc("GET /api/portal/customer/events", s.requireSession(store.RoleCustomer, s.handleAccountEvents))

	mux.HandleFunc("GET /api/portal/provider/me", s.requireSession(store.RoleProvider, s.handleProviderMe))
	mux.HandleFunc("GET /api/portal/provider/nodes", s.requireSession(store.RoleProvider, s.handleListNodes))
	mux.HandleFunc("GET /api/portal/provider/nodes/{id}", s.requireSession(store.RoleProvider, s.handleGetNode))
	mux.HandleFunc("DELETE /api/portal/provider/nodes/{id}", s.requireSession(store.RoleProvider, s.handleDeleteNode))
	mux.HandleFunc("GET /api/portal/provider/nodes/{id}/tasks", s.requireSession(store.RoleProvider, s.handleNodeTasks))
	mux.HandleFunc("POST /api/portal/provider/nodes/install-token", s.requireSession(store.RoleProvider, s.handleInstallToken))
	mux.HandleFunc("GET /api/portal/provider/ledger", s.requireSession(store.RoleProvider, s.handleProviderLedger))
	mux.HandleFunc("GET /api/portal/provider/events", s.requireSession(store.RoleProvider, s.handleAccountEvents))

	// Anything under /api/portal/ that matched no route above is a typo'd or
	// removed endpoint: answer in the same {"error": ...} shape as every other
	// failure instead of net/http's plain-text "404 page not found", which the
	// frontend can't parse.
	mux.HandleFunc("/api/portal/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	return mux
}
