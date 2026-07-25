package api

import (
	"context"
	"net/http"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// HealthProvider is the cached-health surface the health handlers depend on.
// It is satisfied by *sources.HealthCache; the handlers take this interface
// (rather than the concrete type) to match the rest of this package's
// contract-interface handler style and to stay unit-testable with a fake.
type HealthProvider interface {
	// Health returns the cached WorkspaceDetail for projectID, with stale=true
	// when the served value is older than the TTL (a background refresh was
	// started). It blocks only on a cold miss.
	Health(ctx context.Context, projectID string) (contract.WorkspaceDetail, bool, error)
	// Refresh forces a synchronous recompute for projectID and returns the
	// fresh detail (or last-known-good plus an error on failure).
	Refresh(ctx context.Context, projectID string) (contract.WorkspaceDetail, error)
}

// HealthResponse is the body of both health endpoints: the project's per-source
// health plus a stale flag mirroring the X-Cache header.
type HealthResponse struct {
	Health []protocol.PanelSourceHealth `json:"health"`
	Stale  bool                         `json:"stale"`
}

// HealthHandler serves GET /api/v1/projects/{projectId}/health from the cache.
// It never blocks on a cold recompute beyond the single synchronous refresh
// HealthProvider.Health performs on a cold miss; a warm entry is returned
// immediately, and a stale entry is served while a background refresh runs.
// The X-Cache response header is "hit" for a fresh entry and "stale" for a
// served-stale one.
func HealthHandler(provider HealthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		detail, stale, err := provider.Health(r.Context(), id)
		if err != nil {
			writeError(w, listErrorStatus(err), err)
			return
		}
		if stale {
			w.Header().Set("X-Cache", "stale")
		} else {
			w.Header().Set("X-Cache", "hit")
		}
		writeJSON(w, http.StatusOK, HealthResponse{Health: detail.Health, Stale: stale})
	}
}

// HealthRefreshHandler serves POST /api/v1/projects/{projectId}/health/refresh,
// forcing a synchronous recompute and returning the fresh health. The X-Cache
// header is "refresh". If the recompute fails but a last-known-good value
// exists, it is returned with a 200 and X-Cache "stale" rather than an error,
// so an explicit refresh degrades to the cached view instead of a hard fault.
func HealthRefreshHandler(provider HealthProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("projectId")
		detail, err := provider.Refresh(r.Context(), id)
		if err != nil {
			// Refresh returns last-known-good alongside the error when anything
			// is cached (Health non-nil); serve that stale view rather than
			// failing the request. Only a cold failure (no cached health) is a
			// real error.
			if detail.Health == nil {
				writeError(w, listErrorStatus(err), err)
				return
			}
			w.Header().Set("X-Cache", "stale")
			writeJSON(w, http.StatusOK, HealthResponse{Health: detail.Health, Stale: true})
			return
		}
		w.Header().Set("X-Cache", "refresh")
		writeJSON(w, http.StatusOK, HealthResponse{Health: detail.Health, Stale: false})
	}
}
