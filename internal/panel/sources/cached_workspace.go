package sources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
// refresh only when stale (stale-while-revalidate). A process-cold entry
// (nothing in this process's in-memory cache) is first seeded from its
// persisted snapshot on disk, if a previous process ever computed one, so
// only a project that has genuinely never been computed pays a synchronous
// refresh to show the first page. Concurrent refreshes for the same
// project coalesce inside the cache.
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
//
// Every snapshot is also persisted to <project root>/.punakawan/<snapshotFileName>
// (see loadPersistedSnapshot/savePersistedSnapshot) so a `punakawan panel`
// restart - which always starts with an empty in-memory cache - loads each
// project's last-known counts from disk instead of recomputing them live:
// the in-memory cache alone was only warm for the lifetime of one process,
// so every restart paid the full Dolt/bd/git inspection again.
func NewCachedWorkspaceReader(inner contract.WorkspaceReader, reg *registry.Store, primaryID string, ttl time.Duration) *CachedWorkspaceReader {
	c := &CachedWorkspaceReader{inner: inner, registry: reg, primaryID: primaryID}
	refresh := func(ctx context.Context, projectID string) (*snapshot.ProjectSnapshot, error) {
		detail, err := c.inner.Get(ctx, projectID)
		if err != nil {
			return nil, err
		}
		return summaryToSnapshot(detail.WorkspaceSummary), nil
	}
	opts := []snapshot.Option{
		snapshot.WithPersistence(
			func(projectID string) (*snapshot.ProjectSnapshot, bool) { return loadPersistedSnapshot(reg, projectID) },
			func(snap *snapshot.ProjectSnapshot) { savePersistedSnapshot(reg, snap) },
		),
	}
	if ttl > 0 {
		opts = append(opts, snapshot.WithTTL(ttl))
	}
	c.cache = snapshot.New(refresh, opts...)
	return c
}

// snapshotFileName names the per-project persisted ProjectSnapshot file
// within its ".punakawan" directory, alongside workspace.yaml and the
// project's other generated (gitignored) state.
const snapshotFileName = "panel-snapshot.json"

// loadPersistedSnapshot reads projectID's last-saved snapshot from its own
// project root, resolved via reg. ok is false whenever the project is
// unknown to reg, has never been saved, or the file cannot be read/decoded
// - persistence is strictly best-effort, so any of these just falls back
// to the cache's normal cold-start behavior (a live recompute).
func loadPersistedSnapshot(reg *registry.Store, projectID string) (*snapshot.ProjectSnapshot, bool) {
	if reg == nil {
		return nil, false
	}
	entry, err := reg.Get(projectID)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(entry.Path, ".punakawan", snapshotFileName))
	if err != nil {
		return nil, false
	}
	var snap snapshot.ProjectSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, false
	}
	return &snap, true
}

// savePersistedSnapshot durably writes snap to its project's own root
// (resolved via reg), so the next `punakawan panel` process can load it
// back via loadPersistedSnapshot. Writes to a temp file first and renames
// into place so a concurrent loader never observes a half-written file.
// Persistence is best-effort: any failure here (an unregistered project, a
// read-only project root, ...) is silently skipped rather than surfaced,
// since it must never fail the refresh that produced snap.
func savePersistedSnapshot(reg *registry.Store, snap *snapshot.ProjectSnapshot) {
	if reg == nil || snap == nil {
		return
	}
	entry, err := reg.Get(snap.ProjectID)
	if err != nil {
		return
	}
	dir := filepath.Join(entry.Path, ".punakawan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	tmp := filepath.Join(dir, snapshotFileName+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(dir, snapshotFileName))
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
		out = append(out, c.cachedSummary(ctx, e))
	}
	return out, nil
}

// cachedSummary serves one registered project's counts from the snapshot
// cache, joining back the registry metadata the snapshot does not carry.
//
// A process-cold entry (nothing in memory and nothing persisted on disk)
// refreshes once synchronously - deduplicated against the background refresh
// GetOrRefresh just triggered - so a first read shows real data rather than a
// placeholder. If that refresh also fails and nothing is cached, the project
// degrades to an unavailable summary rather than disappearing from the caller's
// result.
func (c *CachedWorkspaceReader) cachedSummary(ctx context.Context, e protocol.PanelWorkspaceRegistryEntry) contract.WorkspaceSummary {
	snap, stale := c.cache.GetOrRefresh(ctx, e.Id)
	if snap == nil && stale {
		if refreshed, refreshErr := c.cache.Refresh(ctx, e.Id); refreshErr == nil {
			snap = refreshed
		}
	}
	if snap == nil {
		return c.unavailableSummary(e)
	}
	return c.snapshotToSummary(snap, e)
}

// Summary returns one project's counts from the snapshot cache, without the
// live per-source Health detail Get computes.
//
// This is the read the project pages actually want. They render counts
// (repositories, knowledge, open/blocked tasks, active sessions) and never
// display per-source health - but recomputing health means opening the
// project's Dolt store, shelling out to `bd list` plus `bd ready`, and running
// `git status` once per repository. Against a project with a real task graph
// that is several seconds of work, and routing the project detail read through
// Get made every single request pay it, even though List had already computed
// and cached the very same counts moments earlier.
//
// Get stays deliberately live because the Health page exists to show fresh
// per-source detail. A counts-only read has no such reason to bypass the
// snapshot, which is background-refreshed on staleness and persisted across
// panel restarts, so the freshness here matches what the project list and
// overview pages already show.
//
// An id the registry does not know yields contract.ErrWorkspaceUnavailable so
// handlers answer 404 rather than 500. With no registry at all, or for the
// workspace this panel instance was itself loaded for before it appears in the
// registry, this falls through to Get: the primary is always resolvable, which
// is the same fallback the inner reader makes.
func (c *CachedWorkspaceReader) Summary(ctx context.Context, workspaceID string) (contract.WorkspaceSummary, error) {
	if c.registry == nil {
		detail, err := c.Get(ctx, workspaceID)
		return detail.WorkspaceSummary, err
	}
	entry, err := c.registry.Get(workspaceID)
	if err != nil {
		if !errors.Is(err, registry.ErrNotFound) {
			return contract.WorkspaceSummary{}, fmt.Errorf("sources: cached workspace summary %q: %w", workspaceID, err)
		}
		if workspaceID == c.primaryID {
			detail, getErr := c.Get(ctx, workspaceID)
			return detail.WorkspaceSummary, getErr
		}
		return contract.WorkspaceSummary{}, fmt.Errorf("sources: workspace %q: %w", workspaceID, contract.ErrWorkspaceUnavailable)
	}
	return c.cachedSummary(ctx, entry), nil
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
