package session

import (
	"encoding/json"
	"net/http"
	"time"
)

// CookieName is the SameSite=Strict, HttpOnly cookie that proves a
// browser has an active panel session.
const CookieName = "punakawan_session"

// CSRFHeader is the header a client must echo the session's CSRF token
// on for every mutating (non-GET/HEAD) request.
const CSRFHeader = "X-Csrf-Token"

type exchangeRequest struct {
	BootstrapToken string `json:"bootstrap_token"`
}

type exchangeResponse struct {
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ExchangeHandler serves POST /api/v1/session/exchange: trades the
// one-time bootstrap token minted at server start for a session cookie
// plus a CSRF token in the response body. The CSRF token cannot itself
// live in the (HttpOnly) cookie - JavaScript must be able to read it to
// echo it back on later requests - so the cookie's only job is proving
// "this browser holds an active session," per §15.
func ExchangeHandler(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req exchangeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BootstrapToken == "" {
			http.Error(w, "session: missing bootstrap_token", http.StatusBadRequest)
			return
		}

		sessionID, csrfToken, expiresAt, err := mgr.ExchangeBootstrapToken(req.BootstrapToken)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     CookieName,
			Value:    sessionID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
			Expires:  expiresAt,
		})

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(exchangeResponse{CSRFToken: csrfToken, ExpiresAt: expiresAt})
	}
}

// RequireSession wraps next so it only runs for requests carrying a live
// session cookie. Non-safe methods (anything but GET/HEAD) must also
// carry a matching CSRFHeader, per §15's "require CSRF tokens for every
// mutation" - GET reads of the same mutation-capable resources (e.g.
// fetching a review to render it) don't need a CSRF token, only a
// session, since they cannot change state.
func RequireSession(mgr *Manager, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieName)
		if err != nil || !mgr.ValidSession(cookie.Value) {
			http.Error(w, "session: missing or expired session", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !mgr.ValidCSRF(cookie.Value, r.Header.Get(CSRFHeader)) {
				http.Error(w, "session: missing or invalid CSRF token", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}
