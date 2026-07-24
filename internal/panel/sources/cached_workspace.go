package sources

import (
	"context"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/snapshot"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CachedWorkspaceReader wraps a WorkspaceReader with a fast in-memory
// snapshot.Cache, per punakawan-panel-project-performance-improvement-plan.md
// §10.2. It exists so /api/v1/workspaces, /api/v1/overview, and the Tier-2
// reconciler serve project counts from a cached snapshot instead of running
// the underlying deep per-workspace inspection (Dolt/bd/git/adapters) on
// every request.
//
// List reads registry entries (cheap) and, for each, serves the cached
// ProjectSnapshot: warm entries return instantly and trigger a background
// refresh only when stale (stale-while-revalidate); a cold entry (nothing
// cached yet) is refreshed once synchronously so the first page still shows
// data. Concurrent refreshes for the same project coalesce inside the cache.
//
// Get delegates to the inner reader for the full WorkspaceDetail (the Health
// page wants live-ish per-source detail the snapshot does not carry) and
// opportunistically warms the cache from that result.
//
// When Registry is nil the wrapper is a transparent passthrough to inner,
// matching the single-workspace fallback the inner reader already supports.
type CachedWorkspaceReader struct {
	inner     contract.WorkspaceReader
	registry  *registry.Store
	primaryID string
	cache     *snapshot.Cache
}

// NewCachedWorkspaceReader builds a CachedWorkspaceReader over inner. reg is
// the registry consulted for the (cheap) list of projects and their display
// metadata; primaryID is the workspace this panel instance was loaded for
// (used to stamp WorkspaceSummary.Primary). ttl overrides the cache
// staleness threshold when > 0.
func NewCachedWorkspaceReader(inner contract.WorkspaceReader, reg *registry.Store, primaryID string, ttl time.Duration) *CachedWorkspaceReader {
	c := &CachedWorkspaceReader{inner: inner, registry: reg, primaryID: primaryID}
	refresh := func(ctx context.Context, projectID string) (*snapshot.ProjectSnapshot, error) {
		detail, err := c.inner.Get(ctx, projectID)
		if err != nil {
			return nil, err
		}
		return summaryToSnapshot(detail.WorkspaceSummary), nil
	}
	var opts []snapshot.Option
	if ttl > 0 {
		opts = append(opts, snapshot.WithTTL(ttl))
	}
	c.cache = snapshot.New(refresh, opts...)
	return c
}

// List returns one WorkspaceSummary per registered project, served from the
// snapshot cache.
func (c *CachedWorkspaceReader) List(ctx context.Context) ([]contract.WorkspaceSummary, error) {
	if c.registry == nil {
		return c.inner.List(ctx)
	}
	entries, err := c.registry.List()
	if err != nil {
		return nil, fmt.Errorf("sources: cached workspace list: %w", err)
	}
	if len(entries) == 0 {
		return c.inner.List(ctx)
	}

	out := make([]contract.WorkspaceSummary, 0, len(entries))
	for _, e := range entries {
		snap, stale := c.cache.GetOrRefresh(ctx, e.Id)
		if snap == nil && stale {
			// Cold entry: refresh once synchronously (deduplicated against the
			// background refresh GetOrRefresh just triggered) so the first page
			// still shows data rather than a placeholder.
			if refreshed, refreshErr := c.cache.Refresh(ctx, e.Id); refreshErr == nil {
				snap = refreshed
			}
		}
		if snap == nil {
			// Refresh failed and nothing is cached: degrade to an unavailable
			// summary rather than dropping the project from the list.
			out = append(out, c.unavailableSummary(e))
			continue
		}
		out = append(out, c.snapshotToSummary(snap, e))
	}
	return out, nil
}

// Get delegates to the inner reader for the full detail and warms the cache
// from the result so a subsequent List for this project is served fresh.
func (c *CachedWorkspaceReader) Get(ctx context.Context, workspaceID string) (contract.WorkspaceDetail, error) {
	detail, err := c.inner.Get(ctx, workspaceID)
	if err != nil {
		return contract.WorkspaceDetail{}, err
	}
	c.cache.Set(summaryToSnapshot(detail.WorkspaceSummary))
	return detail, nil
}

// unavailableSummary builds a placeholder for a project whose snapshot could
// not be computed, carrying the registry metadata that is still known.
func (c *CachedWorkspaceReader) unavailableSummary(e protocol.PanelWorkspaceRegistryEntry) contract.WorkspaceSummary {
	displayName := e.Id
	if e.DisplayName != nil && *e.DisplayName != "" {
		displayName = *e.DisplayName
	}
	return contract.WorkspaceSummary{
		ID:           e.Id,
		Path:         e.Path,
		DisplayName:  displayName,
		Availability: protocol.PanelSourceHealthAvailabilityUnavailable,
		Pinned:       e.Pinned != nil && *e.Pinned,
		Primary:      e.Id == c.primaryID,
	}
}

// snapshotToSummary reconstructs a WorkspaceSummary from a cached snapshot,
// re-joining the registry's authoritative display metadata (path, display
// name, pinned) - which the snapshot deliberately does not carry - and the
// primary flag this instance knows.
func (c *CachedWorkspaceReader) snapshotToSummary(s *snapshot.ProjectSnapshot, e protocol.PanelWorkspaceRegistryEntry) contract.WorkspaceSummary {
	displayName := e.Id
	if e.DisplayName != nil && *e.DisplayName != "" {
		displayName = *e.DisplayName
	}
	return contract.WorkspaceSummary{
		ID:                 s.ProjectID,
		Path:               e.Path,
		DisplayName:        displayName,
		Availability:       protocol.PanelSourceHealthAvailability(s.Availability),
		RepositoryCount:    s.RepositoryCount,
		ActiveSessionCount: s.ActiveRunCount,
		OpenTaskCount:      s.OpenTaskCount,
		BlockedTaskCount:   s.BlockedTaskCount,
		KnowledgeCount:     s.KnowledgeCount,
		LastActivityAt:     s.UpdatedAt,
		Pinned:             e.Pinned != nil && *e.Pinned,
		Primary:            s.ProjectID == c.primaryID,
	}
}

// summaryToSnapshot projects a WorkspaceSummary onto the cache's
// presentation-only ProjectSnapshot. Fields the snapshot does not model
// (path, display name, pinned) are dropped here and re-joined from the
// registry on the way back out; WorkflowCount/PlanCount/PendingReviewCount
// have no source in the current WorkspaceSummary and stay zero until a later
// phase computes them.
func summaryToSnapshot(sum contract.WorkspaceSummary) *snapshot.ProjectSnapshot {
	updated := sum.LastActivityAt
	if updated.IsZero() {
		updated = time.Now().UTC()
	}
	return &snapshot.ProjectSnapshot{
		ProjectID:        sum.ID,
		UpdatedAt:        updated,
		Availability:     string(sum.Availability),
		RepositoryCount:  sum.RepositoryCount,
		KnowledgeCount:   sum.KnowledgeCount,
		ActiveRunCount:   sum.ActiveSessionCount,
		OpenTaskCount:    sum.OpenTaskCount,
		BlockedTaskCount: sum.BlockedTaskCount,
	}
}
