// Package portalapi implements IMPLEMENTATION.md tasks 7.2-7.5: plain
// REST/JSON under /api/portal/*, backing the two portal frontends built in
// Phase 7B (docs/01-dashboard-portals/PROGRESS.md). It wraps the same
// store.Store/api.CustomerServer logic the gRPC servers already use
// (PLAN.md §4) rather than reimplementing task submission, gateway
// creation, or balance checks a second time.
package portalapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/mkassab215/lazycake/internal/coordinator/store"
)

const (
	cookieName = "lazycake_session"
	sessionTTL = 7 * 24 * time.Hour
)

// newSessionID returns a random 32-byte session ID, hex-encoded for use as
// a cookie value.
func newSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hashSessionID is the storage/lookup key for a session ID - same pattern
// auth.Hash uses for bearer tokens (PLAN.md §2: a stolen database backup
// must never yield a usable session any more than a usable token).
func hashSessionID(id string) []byte {
	sum := sha256.Sum256([]byte(id))
	return sum[:]
}

// setSessionCookie issues the httpOnly, SameSite=Lax cookie task 7.2
// specifies. Secure is set unless dev is true (cfg.Dev - local/demo compose
// serves plain HTTP).
func setSessionCookie(w http.ResponseWriter, id string, dev bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   !dev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// clearSessionCookie expires the cookie immediately (logout).
func clearSessionCookie(w http.ResponseWriter, dev bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !dev,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// sessionInfo is what a valid session resolves a request to.
type sessionInfo struct {
	AccountID string
	Role      store.PortalRole
}

type ctxKey int

const sessionCtxKey ctxKey = 0

func withSession(ctx context.Context, s sessionInfo) context.Context {
	return context.WithValue(ctx, sessionCtxKey, s)
}

// sessionFromContext retrieves the session a requireSession middleware
// already validated and attached. Panics if called outside one, since
// that's a programming error (a handler wired up without the middleware),
// not a runtime condition callers should handle.
func sessionFromContext(ctx context.Context) sessionInfo {
	s, ok := ctx.Value(sessionCtxKey).(sessionInfo)
	if !ok {
		panic("portalapi: sessionFromContext called outside requireSession")
	}
	return s
}

// requireSession resolves the session cookie, checks it matches role
// exactly (a customer session must never authorize a provider-only
// endpoint or vice versa), and calls next with the session attached to the
// request's context - task 7.2's session middleware.
func (s *Server) requireSession(role store.PortalRole, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "not signed in")
			return
		}
		sess, err := s.Store.GetSession(r.Context(), hashSessionID(cookie.Value))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session expired or invalid")
			return
		}
		if sess.Role != role {
			// Same message as an invalid session: a provider session
			// hitting a customer endpoint should look the same from the
			// outside as no session at all, not reveal that the cookie
			// was valid for a different role.
			writeError(w, http.StatusUnauthorized, "session expired or invalid")
			return
		}
		next(w, r.WithContext(withSession(r.Context(), sessionInfo{AccountID: sess.AccountID, Role: sess.Role})))
	}
}
