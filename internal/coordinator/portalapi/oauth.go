package portalapi

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fattymango/lazycake/internal/coordinator/ratelimit"
	"github.com/fattymango/lazycake/internal/coordinator/store"
)

// OAuth 2.1 for the MCP server (.claude/mcp.md). The coordinator is both the authorization server and the protected resource: an AI client
// registers itself (RFC 7591), sends the user to the dashboard's /authorize page, the signed-in user approves, and the client exchanges the code
// (PKCE) for a short access token and a rotating refresh token. Public clients only; every token is stored as a hash.

const (
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
	authCodeTTL     = 5 * time.Minute
	accessPrefix    = "lca_"
	refreshPrefix   = "lcr_"
	oauthScope      = "lazycake"
)

// publicBase is the address clients reach the coordinator on, for the metadata documents. LAZYCAKE_PUBLIC_URL wins; otherwise it is read off the
// request (behind a TLS proxy, X-Forwarded-Proto/Host).
func (s *Server) publicBase(r *http.Request) string {
	if s.PublicURL != "" {
		return strings.TrimRight(s.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

// withCORS lets browser-based MCP clients (inspectors) use the discovery, registration, token and MCP endpoints. Safe because none of them
// accepts cookies: access is by bearer token only.
func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Protocol-Version, Mcp-Session-Id, Accept")
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Expose-Headers", "WWW-Authenticate")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicBase(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{oauthScope},
		"resource_name":            "LazyCake",
	})
}

func (s *Server) handleAuthServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicBase(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{oauthScope},
	})
}

type registerRequest struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

// handleRegister is RFC 7591 dynamic client registration: how an AI client introduces itself. It is open (the client has no account yet) so it is
// rate limited per address like sign-up, and it grants nothing: a registered client can do nothing until a signed-in user approves it.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.signupLimit.Allow(ratelimit.ClientIP(r.RemoteAddr)) {
		w.Header().Set("Retry-After", "60")
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many registrations from this address")
		return
	}
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 5 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "between 1 and 5 redirect_uris are required")
		return
	}
	for _, u := range req.RedirectURIs {
		if !validRedirectURI(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect URIs must be https, a loopback http address, or an app scheme")
			return
		}
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "An AI app"
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	c := store.OAuthClient{ClientID: newID("ocl"), ClientName: name, RedirectURIs: req.RedirectURIs}
	if err := s.Store.CreateOAuthClient(r.Context(), c); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "registering the client")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id": c.ClientID, "client_name": c.ClientName, "redirect_uris": c.RedirectURIs,
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none", "client_id_issued_at": time.Now().Unix(),
	})
}

// validRedirectURI accepts https, loopback http (RFC 8252, for desktop and CLI clients) and app-specific schemes ("cursor://..."), never a
// fragment, and never a scheme that runs code in a browser.
func validRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Fragment != "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host != ""
	case "http":
		return isLoopbackHost(u.Hostname())
	case "javascript", "data", "file", "vbscript", "about", "blob", "ftp":
		return false
	}
	return true
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// redirectAllowed is an exact match against the registered URIs, except that a loopback http redirect may use any port (the client picks a free
// one at authorization time, RFC 8252 section 7.3).
func redirectAllowed(registered []string, got string) bool {
	gu, err := url.Parse(got)
	if err != nil {
		return false
	}
	for _, reg := range registered {
		if reg == got {
			return true
		}
		ru, err := url.Parse(reg)
		if err != nil || ru.Scheme != "http" || gu.Scheme != "http" || !isLoopbackHost(ru.Hostname()) || !isLoopbackHost(gu.Hostname()) {
			continue
		}
		if ru.Hostname() == gu.Hostname() && ru.Path == gu.Path && ru.RawQuery == gu.RawQuery {
			return true
		}
	}
	return false
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// --- the consent step (called by the dashboard's /authorize page, with the user's own login session) ---

type authorizeRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	State               string `json:"state"`
	Approve             bool   `json:"approve"`
}

// handleOAuthClient tells the consent page what it is asking the user to approve; it checks the redirect address here so a bad request is shown
// as an error instead of a consent button.
func (s *Server) handleOAuthClient(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.GetOAuthClient(r.Context(), r.URL.Query().Get("client_id"))
	if err != nil || !redirectAllowed(c.RedirectURIs, r.URL.Query().Get("redirect_uri")) {
		writeError(w, http.StatusBadRequest, "This sign-in request is not valid: the app or its return address is not recognised.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"client_name": c.ClientName})
}

// handleOAuthAuthorize records the user's decision. It requires the cookie session (never a bearer token), so an app can not approve itself.
func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	var req authorizeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	c, err := s.Store.GetOAuthClient(r.Context(), req.ClientID)
	if err != nil || !redirectAllowed(c.RedirectURIs, req.RedirectURI) {
		writeError(w, http.StatusBadRequest, "unknown app or return address")
		return
	}
	back, err := url.Parse(req.RedirectURI)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad return address")
		return
	}
	q := back.Query()
	if req.State != "" {
		q.Set("state", req.State)
	}
	if !req.Approve {
		q.Set("error", "access_denied")
		back.RawQuery = q.Encode()
		writeJSON(w, http.StatusOK, map[string]string{"redirect_to": back.String()})
		return
	}
	if req.CodeChallengeMethod != "S256" || len(req.CodeChallenge) < 43 || len(req.CodeChallenge) > 128 {
		writeError(w, http.StatusBadRequest, "the app must use PKCE (code_challenge_method S256)")
		return
	}
	code, hash, err := newSecret("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "approving")
		return
	}
	if err := s.Store.CreateOAuthCode(r.Context(), store.OAuthCode{
		CodeHash: hash, ClientID: c.ClientID, AccountID: sess.AccountID, Role: sess.Role,
		RedirectURI: req.RedirectURI, CodeChallenge: req.CodeChallenge, ExpiresAt: time.Now().Add(authCodeTTL),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "approving")
		return
	}
	q.Set("code", code)
	back.RawQuery = q.Encode()
	writeJSON(w, http.StatusOK, map[string]string{"redirect_to": back.String()})
}

// --- token endpoint ---

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// newID is a random identifier for OAuth rows. (Not internal/id: that package is shared with the agent and gateway, whose release guard would
// then demand a new agent release for a coordinator-only change.)
func newID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func newSecret(prefix string) (value string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", nil, err
	}
	value = prefix + hex.EncodeToString(b)
	return value, hashSessionID(value), nil
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimit.Allow(ratelimit.ClientIP(r.RemoteAddr)) {
		w.Header().Set("Retry-After", "60")
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many requests")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "expected a form-encoded body")
		return
	}
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.exchangeCode(w, r)
	case "refresh_token":
		s.refreshToken(w, r)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (s *Server) exchangeCode(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	code, err := s.Store.ConsumeOAuthCode(r.Context(), hashSessionID(f.Get("code")))
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the code is unknown, expired or already used")
		return
	}
	verifier := f.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if len(verifier) < 43 || len(verifier) > 128 || subtle.ConstantTimeCompare([]byte(challenge), []byte(code.CodeChallenge)) != 1 {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the PKCE verifier does not match")
		return
	}
	if f.Get("client_id") != code.ClientID || f.Get("redirect_uri") != code.RedirectURI {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "client or redirect_uri does not match the approval")
		return
	}
	access, accessHash, err1 := newSecret(accessPrefix)
	refresh, refreshHash, err2 := newSecret(refreshPrefix)
	if err := errors.Join(err1, err2); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "issuing tokens")
		return
	}
	now := time.Now()
	if err := s.Store.CreateOAuthToken(r.Context(), store.OAuthToken{
		ID: newID("ogr"), AccessHash: accessHash, RefreshHash: refreshHash, ClientID: code.ClientID, AccountID: code.AccountID, Role: code.Role,
		AccessExpiresAt: now.Add(accessTokenTTL), RefreshExpiresAt: now.Add(refreshTokenTTL),
	}); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "issuing tokens")
		return
	}
	writeToken(w, access, refresh)
}

func (s *Server) refreshToken(w http.ResponseWriter, r *http.Request) {
	access, accessHash, err1 := newSecret(accessPrefix)
	refresh, refreshHash, err2 := newSecret(refreshPrefix)
	if err := errors.Join(err1, err2); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "issuing tokens")
		return
	}
	now := time.Now()
	_, err := s.Store.RotateOAuthToken(r.Context(), hashSessionID(r.PostForm.Get("refresh_token")), store.OAuthToken{
		ClientID: r.PostForm.Get("client_id"), AccessHash: accessHash, RefreshHash: refreshHash,
		AccessExpiresAt: now.Add(accessTokenTTL), RefreshExpiresAt: now.Add(refreshTokenTTL),
	})
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is unknown, expired or revoked")
		return
	}
	writeToken(w, access, refresh)
}

func writeToken(w http.ResponseWriter, access, refresh string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, tokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(accessTokenTTL.Seconds()), RefreshToken: refresh, Scope: oauthScope})
}

// bearerSession resolves "Authorization: Bearer <access token>" to the account that approved the app.
func (s *Server) bearerSession(r *http.Request) (sessionInfo, bool) {
	h := r.Header.Get("Authorization")
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return sessionInfo{}, false
	}
	t, err := s.Store.GetOAuthTokenByAccess(r.Context(), hashSessionID(strings.TrimSpace(h[7:])))
	if err != nil {
		return sessionInfo{}, false
	}
	return sessionInfo{AccountID: t.AccountID, Role: t.Role}, true
}

// --- connected apps ---

type grantView struct {
	ID           string `json:"id"`
	ClientName   string `json:"client_name"`
	CreatedAtMS  int64  `json:"created_at_ms"`
	LastUsedAtMS int64  `json:"last_used_at_ms,omitempty"`
}

func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	gs, err := s.Store.ListOAuthGrants(r.Context(), sess.AccountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing connected apps")
		return
	}
	out := make([]grantView, len(gs))
	for i, g := range gs {
		out[i] = grantView{ID: g.ID, ClientName: g.ClientName, CreatedAtMS: g.CreatedAt.UnixMilli()}
		if g.LastUsedAt != nil {
			out[i].LastUsedAtMS = g.LastUsedAt.UnixMilli()
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRevokeGrant(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromContext(r.Context())
	if err := s.Store.RevokeOAuthGrant(r.Context(), sess.AccountID, r.PathValue("id")); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "connected app not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "revoking")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
