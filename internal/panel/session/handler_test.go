package session

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func exchange(t *testing.T, mgr *Manager, bootstrapToken string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(exchangeRequest{BootstrapToken: bootstrapToken})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/session/exchange", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ExchangeHandler(mgr).ServeHTTP(rec, req)
	return rec
}

func TestExchangeHandlerSetsSessionCookieAndReturnsCSRFToken(t *testing.T) {
	mgr := NewManager()
	tok, err := mgr.IssueBootstrapToken()
	if err != nil {
		t.Fatalf("IssueBootstrapToken: %v", err)
	}

	rec := exchange(t, mgr, tok)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != CookieName {
		t.Fatalf("cookies = %+v, want one %q cookie", cookies, CookieName)
	}
	if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie = %+v, want HttpOnly + SameSite=Strict", cookies[0])
	}

	var resp exchangeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.CSRFToken == "" {
		t.Fatal("csrf_token missing from exchange response")
	}
	if !mgr.ValidCSRF(cookies[0].Value, resp.CSRFToken) {
		t.Fatal("returned csrf_token does not validate against the issued session")
	}
}

func TestExchangeHandlerRejectsInvalidBootstrapToken(t *testing.T) {
	mgr := NewManager()
	rec := exchange(t, mgr, "not-a-real-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestExchangeHandlerRejectsMissingBootstrapToken(t *testing.T) {
	mgr := NewManager()
	rec := exchange(t, mgr, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestRequireSessionRejectsRequestsWithNoCookie(t *testing.T) {
	mgr := NewManager()
	called := false
	handler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) { called = true })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/review-1", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if called {
		t.Fatal("next was called despite the missing session cookie")
	}
}

func TestRequireSessionAllowsGetWithValidSessionAndNoCSRF(t *testing.T) {
	mgr := NewManager()
	tok, _ := mgr.IssueBootstrapToken()
	sessionID, _, _, err := mgr.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}

	called := false
	handler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) { called = true })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/review-1", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("next was not called despite a valid session")
	}
}

func TestRequireSessionRejectsPostWithoutCSRFHeader(t *testing.T) {
	mgr := NewManager()
	tok, _ := mgr.IssueBootstrapToken()
	sessionID, _, _, err := mgr.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}

	handler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called without a valid CSRF header")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestRequireSessionAllowsPostWithValidCSRFHeader(t *testing.T) {
	mgr := NewManager()
	tok, _ := mgr.IssueBootstrapToken()
	sessionID, csrfToken, _, err := mgr.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}

	called := false
	handler := RequireSession(mgr, func(w http.ResponseWriter, r *http.Request) { called = true })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: sessionID})
	req.Header.Set(CSRFHeader, csrfToken)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("next was not called despite a valid CSRF header")
	}
}
