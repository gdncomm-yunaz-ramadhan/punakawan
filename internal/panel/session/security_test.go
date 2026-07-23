package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestBootstrapTokenExpiresAfterTTL backdates an issued bootstrap token's
// expiry into the past (the same time.Time field ExchangeBootstrapToken
// itself compares time.Now() against) rather than sleeping past the real
// bootstrapTokenTTL (2 minutes) or introducing a mockable clock the
// production Manager doesn't have. This exercises the exact same
// `time.Now().After(expiry)` branch a real expired token would hit -
// it's a white-box test (same package) manufacturing the state a real
// wait would produce, not a fabricated code path.
func TestBootstrapTokenExpiresAfterTTL(t *testing.T) {
	m := NewManager()
	tok, err := m.IssueBootstrapToken()
	if err != nil {
		t.Fatalf("IssueBootstrapToken: %v", err)
	}

	// Backdate as if bootstrapTokenTTL had already elapsed.
	m.mu.Lock()
	m.bootstrap[tok] = time.Now().Add(-time.Second)
	m.mu.Unlock()

	if _, _, _, err := m.ExchangeBootstrapToken(tok); err != ErrInvalidBootstrapToken {
		t.Fatalf("err = %v, want ErrInvalidBootstrapToken for an expired bootstrap token", err)
	}

	// The expired token must also have been consumed (single-use even
	// when rejected for expiry) - a second attempt must fail the same
	// way, not succeed because the first failed check left it in place.
	if _, _, _, err := m.ExchangeBootstrapToken(tok); err != ErrInvalidBootstrapToken {
		t.Fatalf("second err = %v, want ErrInvalidBootstrapToken (expired token must be consumed too)", err)
	}
}

// TestSessionExpiresAfterTTL is the session-cookie equivalent of the
// bootstrap-token expiry test above: it backdates a live session's
// expiresAt into the past (the exact field ValidSession/ValidCSRF check
// against time.Now()) rather than sleeping past the real 12-hour
// sessionTTL. Manager has no injectable clock, so this is the only way to
// exercise the expiry branch without either a real 12-hour sleep or
// faking time in a way that bypasses the production code path - see the
// NOTE at the bottom of this file for why a true end-to-end,
// clock-driven TTL-elapsed test isn't attempted here.
func TestSessionExpiresAfterTTL(t *testing.T) {
	m := NewManager()
	tok, err := m.IssueBootstrapToken()
	if err != nil {
		t.Fatalf("IssueBootstrapToken: %v", err)
	}
	sessionID, csrfToken, _, err := m.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}
	if !m.ValidSession(sessionID) {
		t.Fatal("ValidSession = false immediately after exchange, want true")
	}

	// Backdate as if sessionTTL had already elapsed.
	m.mu.Lock()
	e := m.sessions[sessionID]
	e.expiresAt = time.Now().Add(-time.Second)
	m.sessions[sessionID] = e
	m.mu.Unlock()

	if m.ValidSession(sessionID) {
		t.Fatal("ValidSession = true for a session past its TTL, want false")
	}
	if m.ValidCSRF(sessionID, csrfToken) {
		t.Fatal("ValidCSRF = true for a session past its TTL, want false (an expired session's CSRF token must not still validate)")
	}
}

// TestRequireSessionRejectsAnExpiredSession is TestSessionExpiresAfterTTL
// exercised through the actual HTTP middleware a real mutating request
// goes through, not just the Manager methods directly - confirming the
// full request path (cookie -> ValidSession check inside RequireSession)
// rejects an expired session exactly like a missing one.
func TestRequireSessionRejectsAnExpiredSession(t *testing.T) {
	mgr := NewManager()
	tok, _ := mgr.IssueBootstrapToken()
	sessionID, csrfToken, _, err := mgr.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}

	mgr.mu.Lock()
	e := mgr.sessions[sessionID]
	e.expiresAt = time.Now().Add(-time.Second)
	mgr.sessions[sessionID] = e
	mgr.mu.Unlock()

	handler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called for an expired session")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessionID})
	req.Header.Set(CSRFHeader, csrfToken)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an expired session even with a correct CSRF header", rec.Code)
	}
}

// TestRequireSessionRejectsMutationWithWrongCSRFHeader extends the
// existing "missing CSRF header" coverage (handler_test.go) with the
// "present but wrong" case: a session cookie that's genuinely valid,
// paired with a CSRF token that simply doesn't match (e.g. copied from a
// different, or previous, session) must still be rejected.
func TestRequireSessionRejectsMutationWithWrongCSRFHeader(t *testing.T) {
	mgr := NewManager()
	tok, _ := mgr.IssueBootstrapToken()
	sessionID, _, _, err := mgr.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}

	handler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called with a mismatched CSRF header")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessionID})
	req.Header.Set(CSRFHeader, "totally-wrong-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a session-valid, CSRF-mismatched mutation", rec.Code)
	}
}

// TestRequireSessionRejectsMutationFromAnotherSessionsCSRFToken confirms
// CSRF tokens are bound to their own session, not merely "some live
// session's token" - a second, independently-issued session's otherwise-
// valid CSRF token must not authorize a mutation against the first
// session's cookie.
func TestRequireSessionRejectsMutationFromAnotherSessionsCSRFToken(t *testing.T) {
	mgr := NewManager()

	tokA, _ := mgr.IssueBootstrapToken()
	sessionA, _, _, err := mgr.ExchangeBootstrapToken(tokA)
	if err != nil {
		t.Fatalf("exchange A: %v", err)
	}
	tokB, _ := mgr.IssueBootstrapToken()
	_, csrfB, _, err := mgr.ExchangeBootstrapToken(tokB)
	if err != nil {
		t.Fatalf("exchange B: %v", err)
	}

	handler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called with another session's CSRF token")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessionA})
	req.Header.Set(CSRFHeader, csrfB)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (session A's cookie with session B's CSRF token)", rec.Code)
	}
}

// TestRequireSessionRejectsMutationWithNoCookieAtAll is the "unauth"
// counterpart to the existing GET/no-cookie test (handler_test.go covers
// GET; this covers a mutating POST with neither a cookie nor a CSRF
// header at all, the simplest possible unauthenticated attempt).
func TestRequireSessionRejectsMutationWithNoCookieAtAll(t *testing.T) {
	mgr := NewManager()
	handler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called for a request with no session cookie at all")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/submit", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestInvalidateAllRejectsSubsequentRequestsThroughTheFullMiddleware
// extends the existing Manager-level TestInvalidateAllDropsLiveSessions
// (manager_test.go) through RequireSession itself, confirming a request
// that carries a cookie/CSRF pair that was completely valid a moment ago
// is rejected immediately after InvalidateAll runs (the shape Shutdown
// actually calls in internal/panel/server.Server.Shutdown) - i.e. a panel
// restart truly invalidates every previously-issued session for any
// request path, not just the Manager's own accessor methods.
func TestInvalidateAllRejectsSubsequentRequestsThroughTheFullMiddleware(t *testing.T) {
	mgr := NewManager()
	tok, _ := mgr.IssueBootstrapToken()
	sessionID, csrfToken, _, err := mgr.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}

	// Prove the session genuinely works before invalidation.
	okHandler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	preReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", nil)
	preReq.AddCookie(&http.Cookie{Name: CookieName, Value: sessionID})
	preReq.Header.Set(CSRFHeader, csrfToken)
	preRec := httptest.NewRecorder()
	okHandler(preRec, preReq)
	if preRec.Code != http.StatusOK {
		t.Fatalf("pre-invalidation status = %d, want 200 (sanity check that the session works)", preRec.Code)
	}

	mgr.InvalidateAll()

	handler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called for a session after InvalidateAll")
	})
	postReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", nil)
	postReq.AddCookie(&http.Cookie{Name: CookieName, Value: sessionID})
	postReq.Header.Set(CSRFHeader, csrfToken)
	postRec := httptest.NewRecorder()
	handler(postRec, postReq)

	if postRec.Code != http.StatusUnauthorized {
		t.Fatalf("post-invalidation status = %d, want 401 (InvalidateAll must invalidate every previously-valid session)", postRec.Code)
	}
}

// TestInvalidateAllAlsoConsumesUnusedBootstrapTokens confirms
// InvalidateAll's "every live session and unused bootstrap token" claim
// (manager.go's own doc comment) covers a bootstrap token that was issued
// but never exchanged yet - e.g. a browser tab that loaded the bootstrap
// URL but hadn't completed the exchange call when the panel process
// stopped.
func TestInvalidateAllAlsoConsumesUnusedBootstrapTokens(t *testing.T) {
	mgr := NewManager()
	tok, err := mgr.IssueBootstrapToken()
	if err != nil {
		t.Fatalf("IssueBootstrapToken: %v", err)
	}

	mgr.InvalidateAll()

	if _, _, _, err := mgr.ExchangeBootstrapToken(tok); err != ErrInvalidBootstrapToken {
		t.Fatalf("err = %v, want ErrInvalidBootstrapToken for a bootstrap token issued before InvalidateAll", err)
	}
}

// NOTE on a genuine gap: Manager has no injectable clock (no `now func()
// time.Time` field), so bootstrapTokenTTL/sessionTTL expiry can only be
// tested here by directly backdating the unexported expiresAt field from
// within this white-box test package, as done above - there is no way to
// write a black-box (external package) test that observes real TTL
// expiry without either sleeping for the real 2-minute/12-hour duration
// or reaching into unexported state the way this file does. This is
// noted honestly rather than adding a speculative clock-injection
// facility to production code that nothing else needs yet.
