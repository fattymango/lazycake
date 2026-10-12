package portalapi

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fattymango/lazycake/internal/coordinator/api"
	"github.com/fattymango/lazycake/internal/coordinator/events"
	"github.com/fattymango/lazycake/internal/coordinator/ratelimit"
	"github.com/fattymango/lazycake/internal/coordinator/store"
)

// Server implements every /api/portal/* handler (IMPLEMENTATION.md tasks
// 7.2-7.5). It wraps api.CustomerServer for task submission (task 7.4:
// "don't duplicate the digest-pin/gateway-ownership/balance checks") and
// store.Store directly for everything account-scoped enough that no
// existing gRPC method already does it.
// GatewayDisconnector closes a gateway's live connection because the customer deleted it.
type GatewayDisconnector interface {
	DisconnectGateway(gatewayID string) bool
}

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
	// Prober checks a gateway's connectivity for the gateway "Test connection"
	// button (the tunnel relay in production). Optional: without it the test
	// endpoint answers 503.
	Prober GatewayProber
	// Disconnector drops a deleted gateway's live relay connection (the tunnel relay in production). Optional.
	Disconnector GatewayDisconnector
	Log          *slog.Logger
	// Dev mirrors config.Config.Dev: the session cookie is Secure unless
	// this is true (PLAN.md §2 - local/demo compose serves plain HTTP).
	Dev bool
	// CoordinatorAddr is the address an agent should be told to dial,
	// used only to build the install command task 7.5's "add a machine"
	// page shows (host:port an operator's machine can actually reach -
	// not necessarily cfg.GRPCAddr's bind address verbatim behind NAT or
	// a reverse proxy; see OPEN_QUESTIONS.md).
	CoordinatorAddr string
	// PublicURL is the https://host the dashboard is reached on, for the OAuth/MCP discovery documents (LAZYCAKE_PUBLIC_URL). Empty: read from each
	// request (and X-Forwarded-Proto/Host behind a proxy).
	PublicURL string
	// Version is reported to MCP clients as the server version.
	Version string

	limiter *loginLimiter

	apiOnce sync.Once
	api     http.Handler

	// Per-IP request limits (see ratelimit). Built by Handler; tests may replace them first.
	reqLimit    *ratelimit.Limiter // every /api/portal/ request
	loginLimit  *ratelimit.Limiter // login attempts, whether or not the password was right
	signupLimit *ratelimit.Limiter // account creation
}

// Handler returns the mux to mount at /api/portal/ on the coordinator's
// existing HTTP server, alongside the operator dashboard
// (cmd/coordinator/run.go). Every path here already starts with
// /api/portal/, so mounting it does not touch any of the dashboard's own
// routes; relocating the dashboard itself to /ops and serving the built
// portal frontends is task 7.10, still open (see
// docs/01-dashboard-portals/PROGRESS.md).
func (s *Server) Handler() http.Handler {
	s.initLimits()
	mux := http.NewServeMux()
	s.routes(mux)
	return s.rateLimited(mux)
}

// apiHandler is Handler built once, for the MCP tools that call the REST API in-process.
func (s *Server) apiHandler() http.Handler {
	s.apiOnce.Do(func() { s.api = s.Handler() })
	return s.api
}

func (s *Server) initLimits() {
	if s.limiter == nil {
		s.limiter = newLoginLimiter()
	}

	if s.reqLimit == nil {
		// The dashboard polls several endpoints and holds one SSE stream; this leaves a normal tab far below the limit.
		s.reqLimit = ratelimit.NewLimiter(30, time.Second, 120)
	}
	if s.loginLimit == nil {
		s.loginLimit = ratelimit.NewLimiter(10, time.Minute, 10)
	}
	if s.signupLimit == nil {
		s.signupLimit = ratelimit.NewLimiter(5, 10*time.Minute, 5)
	}
}

func (s *Server) routes(mux *http.ServeMux) {

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
	mux.HandleFunc("GET /api/portal/customer/tasks/summary", s.requireSession(store.RoleCustomer, s.handleTaskSummary))
	mux.HandleFunc("GET /api/portal/customer/tasks/{id}", s.requireSession(store.RoleCustomer, s.handleGetTask))
	mux.HandleFunc("POST /api/portal/customer/tasks/{id}/cancel", s.requireSession(store.RoleCustomer, s.handleCancelTask))
	mux.HandleFunc("GET /api/portal/customer/tasks/{id}/logs", s.requireSession(store.RoleCustomer, s.handleTaskLogs))
	mux.HandleFunc("GET /api/portal/customer/gateways", s.requireSession(store.RoleCustomer, s.handleListGateways))
	mux.HandleFunc("GET /api/portal/customer/gateways/rate", s.requireSession(store.RoleCustomer, s.handleGatewayRate))
	mux.HandleFunc("GET /api/portal/customer/gateways/{id}/traffic", s.requireSession(store.RoleCustomer, s.handleGatewayTraffic))
	mux.HandleFunc("GET /api/portal/customer/tasks/{id}/usage", s.requireSession(store.RoleCustomer, s.handleTaskUsage))
	mux.HandleFunc("GET /api/portal/customer/tasks/{id}/traffic", s.requireSession(store.RoleCustomer, s.handleTaskTraffic))
	mux.HandleFunc("POST /api/portal/customer/gateways/{id}/test", s.requireSession(store.RoleCustomer, s.handleTestGateway))
	mux.HandleFunc("DELETE /api/portal/customer/gateways/{id}", s.requireSession(store.RoleCustomer, s.handleDeleteGateway))
	mux.HandleFunc("POST /api/portal/customer/gateways", s.requireSession(store.RoleCustomer, s.handleCreateGateway))
	mux.HandleFunc("GET /api/portal/customer/ledger", s.requireSession(store.RoleCustomer, s.handleCustomerLedger))
	mux.HandleFunc("POST /api/portal/customer/balance/add", s.requireSession(store.RoleCustomer, s.handleAddBalance))
	mux.HandleFunc("GET /api/portal/customer/events", s.requireSession(store.RoleCustomer, s.handleAccountEvents))

	mux.HandleFunc("GET /api/portal/provider/me", s.requireSession(store.RoleProvider, s.handleProviderMe))
	mux.HandleFunc("GET /api/portal/provider/nodes", s.requireSession(store.RoleProvider, s.handleListNodes))
	mux.HandleFunc("GET /api/portal/provider/nodes/{id}", s.requireSession(store.RoleProvider, s.handleGetNode))
	mux.HandleFunc("DELETE /api/portal/provider/nodes/{id}", s.requireSession(store.RoleProvider, s.handleDeleteNode))
	mux.HandleFunc("GET /api/portal/provider/nodes/{id}/usage", s.requireSession(store.RoleProvider, s.handleNodeUsage))
	mux.HandleFunc("GET /api/portal/provider/nodes/{id}/tasks", s.requireSession(store.RoleProvider, s.handleNodeTasks))
	mux.HandleFunc("POST /api/portal/provider/nodes/install-token", s.requireSession(store.RoleProvider, s.handleInstallToken))
	mux.HandleFunc("GET /api/portal/provider/ledger", s.requireSession(store.RoleProvider, s.handleProviderLedger))
	mux.HandleFunc("GET /api/portal/provider/events", s.requireSession(store.RoleProvider, s.handleAccountEvents))

	// Connecting AI apps (MCP): the consent step and the list of connected apps. Cookie session only, never a bearer token, so an app cannot
	// approve itself or hide from the list.
	mux.HandleFunc("GET /api/portal/oauth/client", s.requireAnySession(s.handleOAuthClient))
	mux.HandleFunc("POST /api/portal/oauth/authorize", s.requireAnySession(s.handleOAuthAuthorize))
	mux.HandleFunc("GET /api/portal/oauth/grants", s.requireAnySession(s.handleListGrants))
	mux.HandleFunc("DELETE /api/portal/oauth/grants/{id}", s.requireAnySession(s.handleRevokeGrant))

	// Anything under /api/portal/ that matched no route above is a typo'd or
	// removed endpoint: answer in the same {"error": ...} shape as every other
	// failure instead of net/http's plain-text "404 page not found", which the
	// frontend can't parse.
	mux.HandleFunc("/api/portal/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
}

// MCPHandler serves what an AI client talks to, outside /api/portal/: the MCP endpoint itself, its OAuth discovery documents, dynamic client
// registration and the token endpoint. Mount it for /mcp, /oauth/ and /.well-known/ (see .claude/mcp.md).
func (s *Server) MCPHandler() http.Handler {
	s.initLimits()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-protected-resource", withCORS(s.handleProtectedResourceMetadata))
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", withCORS(s.handleProtectedResourceMetadata))
	mux.HandleFunc("/.well-known/oauth-authorization-server", withCORS(s.handleAuthServerMetadata))
	mux.HandleFunc("OPTIONS /oauth/register", withCORS(nil))
	mux.HandleFunc("POST /oauth/register", withCORS(s.handleRegister))
	mux.HandleFunc("OPTIONS /oauth/token", withCORS(nil))
	mux.HandleFunc("POST /oauth/token", withCORS(s.handleToken))
	mux.HandleFunc("/mcp", withCORS(s.handleMCP))
	return mux
}

// rateLimited throttles by client IP: everything under /api/portal/ gets a generous limit, and login and signup a
// strict one on top (the per-username lockout alone lets one address try a password against every account).
func (s *Server) rateLimited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ratelimit.ClientIP(r.RemoteAddr)
		limit := s.reqLimit
		if r.Method == http.MethodPost {
			switch {
			case strings.HasSuffix(r.URL.Path, "/login"):
				limit = s.loginLimit
			case strings.HasSuffix(r.URL.Path, "/signup"):
				limit = s.signupLimit
			}
		}
		if !s.reqLimit.Allow(ip) || (limit != s.reqLimit && !limit.Allow(ip)) {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "too many requests: slow down and try again in a minute")
			return
		}
		next.ServeHTTP(w, r)
	})
}
