package sources

import (
	"context"
	"sync"
	"time"

	"github.com/ygrip/punakawan/internal/panel/contract"
)

// DefaultHealthTTL is the staleness threshold for the cached health layer, per
// the panel performance plan §13.1's "15-30s" guidance. Health probes (git
// status per repo, adapter LookPath, knowledge/bd/workflow checks) are
// expensive, so the /health endpoint serves this cache instead of recomputing
// on every request.
const DefaultHealthTTL = 30 * time.Second

// healthEntry is one project's last-computed WorkspaceDetail and when it was
// stored, used to decide staleness against the TTL.
type healthEntry struct {
	detail    contract.WorkspaceDetail
	updatedAt time.Time
}

// healthCall is one in-progress refresh for a projectID, used to coalesce
// concurrent refreshers (singleflight) so the expensive inner.Get runs once.
type healthCall struct {
	done   chan struct{}
	detail contract.WorkspaceDetail
	err    error
}

// HealthCache wraps a contract.WorkspaceReader with a per-project cached
// WorkspaceDetail and a TTL, per the panel performance plan §13.1. It serves
// the /health endpoint's per-source PanelSourceHealth without re-running the
// underlying deep inspection (git/adapters/knowledge/bd/workflow) on every
// request, using stale-while-revalidate: a fresh entry is returned directly, a
// stale entry is returned immediately while a single background refresh runs,
// and a cold miss is refreshed once synchronously so the first request still
// gets data. A failed refresh preserves the last successful result ("preserve
// last successful result if refresh times out").
//
// Concurrent refreshes for the same project coalesce onto one inner.Get
// (inline singleflight; no golang.org/x/sync dependency). The clock is
// injectable for deterministic tests.
type HealthCache struct {
	inner contract.WorkspaceReader
	ttl   time.Duration
	now   func() time.Time

	mu    sync.Mutex
	cache map[string]healthEntry

	flightMu sync.Mutex
	flight   map[string]*healthCall
}

// HealthCacheOption configures a HealthCache.
type HealthCacheOption func(*HealthCache)

// WithHealthClock injects a clock for deterministic tests. A nil now leaves
// the default (time.Now).
func WithHealthClock(now func() time.Time) HealthCacheOption {
	return func(h *HealthCache) {
		if now != nil {
			h.now = now
		}
	}
}

// NewHealthCache builds a HealthCache over inner. A non-positive ttl uses
// DefaultHealthTTL.
func NewHealthCache(inner contract.WorkspaceReader, ttl time.Duration, opts ...HealthCacheOption) *HealthCache {
	if ttl <= 0 {
		ttl = DefaultHealthTTL
	}
	h := &HealthCache{
		inner:  inner,
		ttl:    ttl,
		now:    time.Now,
		cache:  make(map[string]healthEntry),
		flight: make(map[string]*healthCall),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Health returns the cached WorkspaceDetail for projectID. The stale return is
// true when the served detail is older than the TTL (a background refresh was
// kicked off) so the handler can surface an X-Cache: stale hint. A cold miss
// (nothing cached) is refreshed once synchronously; that single blocking
// recompute is the only time Health waits on the inner reader. On a cold-miss
// refresh error Health returns that error; once anything is cached, Health
// never fails (it serves last-known-good and refreshes in the background).
func (h *HealthCache) Health(ctx context.Context, projectID string) (contract.WorkspaceDetail, bool, error) {
	h.mu.Lock()
	entry, ok := h.cache[projectID]
	h.mu.Unlock()

	if !ok {
		// Cold miss: one synchronous refresh so the first request gets data.
		// coldOnly=true so concurrent cold misses coalesce: whichever caller
		// wins the singleflight computes once, and a straggler that only enters
		// doRefresh after the leader has already populated the cache serves that
		// fresh entry instead of firing a redundant recompute.
		detail, err := h.doRefresh(ctx, projectID, true)
		if err != nil {
			return contract.WorkspaceDetail{}, false, err
		}
		return detail, false, nil
	}

	if h.now().Sub(entry.updatedAt) >= h.ttl {
		// Serve stale immediately and revalidate in the background.
		h.triggerBackgroundRefresh(projectID)
		return entry.detail, true, nil
	}
	return entry.detail, false, nil
}

// Refresh forces a synchronous recompute of projectID (for the explicit
// /health/refresh endpoint), coalescing against any in-flight refresh. On
// success it returns the fresh detail; on error it returns the last-known-good
// detail (if any) plus the error, leaving the cache untouched.
func (h *HealthCache) Refresh(ctx context.Context, projectID string) (contract.WorkspaceDetail, error) {
	detail, err := h.doRefresh(ctx, projectID, false)
	if err != nil {
		h.mu.Lock()
		entry, ok := h.cache[projectID]
		h.mu.Unlock()
		if ok {
			return entry.detail, err
		}
		return contract.WorkspaceDetail{}, err
	}
	return detail, nil
}

// Invalidate drops any cached detail for projectID, forcing the next Health to
// treat it as a cold miss.
func (h *HealthCache) Invalidate(projectID string) {
	h.mu.Lock()
	delete(h.cache, projectID)
	h.mu.Unlock()
}

// triggerBackgroundRefresh runs a refresh for projectID in a goroutine unless
// one is already in flight, so a stale read never blocks the caller and
// concurrent stale reads do not stack up redundant recomputes.
func (h *HealthCache) triggerBackgroundRefresh(projectID string) {
	h.flightMu.Lock()
	_, running := h.flight[projectID]
	h.flightMu.Unlock()
	if running {
		return
	}
	// Stale revalidation always recomputes (coldOnly=false): the entry exists
	// but is aged, so a present-cache short-circuit would defeat the refresh.
	go func() { _, _ = h.doRefresh(context.Background(), projectID, false) }()
}

// doRefresh runs inner.Get for projectID, coalescing concurrent callers so
// only one inner.Get runs per project at a time (inline singleflight). On
// success it stores the result under the cache clock; on error the previously
// cached entry (if any) is left untouched, preserving last-known-good.
//
// When coldOnly is true the leader re-checks the cache under the flight lock
// before recomputing and, if an entry now exists, serves it without a fresh
// inner.Get - this closes the cold-miss race where a caller read an empty
// cache but only reached doRefresh after another caller had already populated
// it. Stale revalidation and explicit Refresh pass coldOnly=false so they
// always recompute.
func (h *HealthCache) doRefresh(ctx context.Context, projectID string, coldOnly bool) (contract.WorkspaceDetail, error) {
	h.flightMu.Lock()
	if c, ok := h.flight[projectID]; ok {
		h.flightMu.Unlock()
		<-c.done
		return c.detail, c.err
	}
	if coldOnly {
		h.mu.Lock()
		entry, cached := h.cache[projectID]
		h.mu.Unlock()
		if cached {
			h.flightMu.Unlock()
			return entry.detail, nil
		}
	}
	c := &healthCall{done: make(chan struct{})}
	h.flight[projectID] = c
	h.flightMu.Unlock()

	c.detail, c.err = h.inner.Get(ctx, projectID)
	if c.err == nil {
		h.mu.Lock()
		h.cache[projectID] = healthEntry{detail: c.detail, updatedAt: h.now()}
		h.mu.Unlock()
	}

	h.flightMu.Lock()
	delete(h.flight, projectID)
	h.flightMu.Unlock()
	close(c.done)
	return c.detail, c.err
}
