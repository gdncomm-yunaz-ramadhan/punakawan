package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeHealthProvider is an in-memory HealthProvider recording call counts so
// tests can assert the handlers hit the cache versus force a refresh.
type fakeHealthProvider struct {
	detail       contract.WorkspaceDetail
	stale        bool
	healthErr    error
	refreshErr   error
	healthCalls  int
	refreshCalls int
}

func (f *fakeHealthProvider) Health(ctx context.Context, projectID string) (contract.WorkspaceDetail, bool, error) {
	f.healthCalls++
	if f.healthErr != nil {
		return contract.WorkspaceDetail{}, false, f.healthErr
	}
	return f.detail, f.stale, nil
}

func (f *fakeHealthProvider) Refresh(ctx context.Context, projectID string) (contract.WorkspaceDetail, error) {
	f.refreshCalls++
	if f.refreshErr != nil {
		return f.detail, f.refreshErr
	}
	return f.detail, nil
}

func healthFixture() contract.WorkspaceDetail {
	return contract.WorkspaceDetail{
		WorkspaceSummary: contract.WorkspaceSummary{ID: "proj-a"},
		Health: []protocol.PanelSourceHealth{{
			Source:       "git",
			Availability: protocol.PanelSourceHealthAvailabilityAvailable,
			CheckedAt:    time.Unix(1000, 0).UTC(),
		}},
	}
}

func TestHealthHandlerReturnsCached(t *testing.T) {
	provider := &fakeHealthProvider{detail: healthFixture()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/health", nil)
	req.SetPathValue("projectId", "proj-a")
	rec := httptest.NewRecorder()
	HealthHandler(provider)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Cache"); got != "hit" {
		t.Fatalf("X-Cache = %q, want hit", got)
	}
	var body HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Stale || len(body.Health) != 1 || body.Health[0].Source != "git" {
		t.Fatalf("body = %+v, want fresh git health", body)
	}
	if provider.healthCalls != 1 || provider.refreshCalls != 0 {
		t.Fatalf("calls = (health %d, refresh %d), want (1, 0)", provider.healthCalls, provider.refreshCalls)
	}
}

func TestHealthHandlerStaleHeader(t *testing.T) {
	provider := &fakeHealthProvider{detail: healthFixture(), stale: true}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/health", nil)
	req.SetPathValue("projectId", "proj-a")
	rec := httptest.NewRecorder()
	HealthHandler(provider)(rec, req)

	if got := rec.Header().Get("X-Cache"); got != "stale" {
		t.Fatalf("X-Cache = %q, want stale", got)
	}
	var body HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Stale {
		t.Fatal("body.Stale = false, want true")
	}
}

func TestHealthHandlerUnknownReturns404(t *testing.T) {
	provider := &fakeHealthProvider{healthErr: contract.ErrWorkspaceUnavailable}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nope/health", nil)
	req.SetPathValue("projectId", "nope")
	rec := httptest.NewRecorder()
	HealthHandler(provider)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHealthRefreshHandlerForcesRefresh(t *testing.T) {
	provider := &fakeHealthProvider{detail: healthFixture()}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/proj-a/health/refresh", nil)
	req.SetPathValue("projectId", "proj-a")
	rec := httptest.NewRecorder()
	HealthRefreshHandler(provider)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Cache"); got != "refresh" {
		t.Fatalf("X-Cache = %q, want refresh", got)
	}
	var body HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Stale || len(body.Health) != 1 {
		t.Fatalf("body = %+v, want fresh health", body)
	}
	if provider.refreshCalls != 1 || provider.healthCalls != 0 {
		t.Fatalf("calls = (health %d, refresh %d), want (0, 1)", provider.healthCalls, provider.refreshCalls)
	}
}

func TestHealthRefreshHandlerErrorWithLastGoodServesStale(t *testing.T) {
	// Refresh failed but returned last-known-good (Health non-nil): degrade to
	// the cached view with a 200 rather than a hard error.
	provider := &fakeHealthProvider{detail: healthFixture(), refreshErr: errors.New("boom")}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/proj-a/health/refresh", nil)
	req.SetPathValue("projectId", "proj-a")
	rec := httptest.NewRecorder()
	HealthRefreshHandler(provider)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (degrade to last-good)", rec.Code)
	}
	if got := rec.Header().Get("X-Cache"); got != "stale" {
		t.Fatalf("X-Cache = %q, want stale", got)
	}
}

func TestHealthRefreshHandlerColdErrorFails(t *testing.T) {
	// Refresh failed with no cached health (cold): a real error.
	provider := &fakeHealthProvider{refreshErr: contract.ErrWorkspaceUnavailable}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/nope/health/refresh", nil)
	req.SetPathValue("projectId", "nope")
	rec := httptest.NewRecorder()
	HealthRefreshHandler(provider)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
