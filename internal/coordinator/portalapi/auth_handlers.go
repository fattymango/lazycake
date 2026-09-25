package portalapi

import (
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
	"github.com/mkassab215/lazycake/internal/id"
)

const minPasswordLen = 8

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	AccountID string           `json:"account_id"`
	Username  string           `json:"username"`
	Role      store.PortalRole `json:"role"`
}

// handleSignup implements task 7.2's POST /api/portal/{customer,provider}/signup:
// create the account plus its portal_credentials row, then sign the caller
// straight in (create session, set cookie) - "register -> land signed in"
// (task 7.7's own "Done when").
func (s *Server) handleSignup(role store.PortalRole) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req credentialsRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.Username == "" {
			writeError(w, http.StatusBadRequest, "username is required")
			return
		}
		if len(req.Password) < minPasswordLen {
			writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "creating account")
			return
		}

		accountID := id.New(id.Account)
		if err := s.Store.CreateAccount(r.Context(), store.Account{ID: accountID, Name: req.Username}); err != nil {
			writeError(w, http.StatusInternalServerError, "creating account")
			return
		}
		if err := s.Store.CreatePortalCredential(r.Context(), store.PortalCredential{
			AccountID: accountID, Username: req.Username, PasswordHash: string(hash), Role: role,
		}); err != nil {
			if err == store.ErrDuplicate {
				writeError(w, http.StatusConflict, "username already taken")
				return
			}
			writeError(w, http.StatusInternalServerError, "creating account")
			return
		}

		if !s.startSession(w, r, accountID, role) {
			return
		}
		writeJSON(w, http.StatusCreated, authResponse{AccountID: accountID, Username: req.Username, Role: role})
	}
}

// handleLogin implements task 7.2's POST /api/portal/{customer,provider}/login:
// one generic "invalid username or password" error on any failure (wrong
// password, unknown username, or a locked-out username) so a caller can
// never distinguish which usernames exist.
func (s *Server) handleLogin(role store.PortalRole) http.HandlerFunc {
	const genericError = "invalid username or password"

	return func(w http.ResponseWriter, r *http.Request) {
		var req credentialsRequest
		if !decodeJSON(w, r, &req) {
			return
		}

		if s.limiter.Locked(req.Username) {
			writeError(w, http.StatusUnauthorized, genericError)
			return
		}

		cred, err := s.Store.GetPortalCredentialByUsername(r.Context(), req.Username)
		if err != nil || cred.Role != role {
			// A credential registered for the other portal must fail the
			// same way as an unknown username - it reveals nothing about
			// which usernames exist, on which portal.
			s.limiter.RecordFailure(req.Username)
			writeError(w, http.StatusUnauthorized, genericError)
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(cred.PasswordHash), []byte(req.Password)); err != nil {
			s.limiter.RecordFailure(req.Username)
			writeError(w, http.StatusUnauthorized, genericError)
			return
		}
		s.limiter.RecordSuccess(req.Username)

		if !s.startSession(w, r, cred.AccountID, role) {
			return
		}
		writeJSON(w, http.StatusOK, authResponse{AccountID: cred.AccountID, Username: cred.Username, Role: role})
	}
}

// handleLogout implements POST /api/portal/logout: revoke the session (if
// any) and clear the cookie either way, so a request with no cookie, a
// stale cookie, or a valid one all succeed identically.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil && cookie.Value != "" {
		if err := s.Store.RevokeSession(r.Context(), hashSessionID(cookie.Value)); err != nil {
			s.Log.Warn("revoking session on logout", "error", err)
		}
	}
	clearSessionCookie(w, s.Dev)
	w.WriteHeader(http.StatusNoContent)
}

// startSession creates a session row and sets the cookie, writing a 500 and
// returning false on failure so the caller can bail out of the handler.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, accountID string, role store.PortalRole) bool {
	sessID, err := newSessionID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating session")
		return false
	}
	if err := s.Store.CreateSession(r.Context(), store.Session{
		IDHash: hashSessionID(sessID), AccountID: accountID, Role: role,
		ExpiresAt: time.Now().Add(sessionTTL),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "creating session")
		return false
	}
	setSessionCookie(w, sessID, s.Dev)
	return true
}
