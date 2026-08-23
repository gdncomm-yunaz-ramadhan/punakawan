// Package snapshot provides a fast, in-memory per-project overview cache
// with stale-while-revalidate semantics, per
// punakawan-panel-project-performance-improvement-plan.md §10/§10.2/§10.4.
//
// The panel's project overview is expensive to compute from scratch: it
// aggregates repository, knowledge, workflow, plan, run, task, and review
// counts across a workspace's Dolt/bd/git/adapter state. Recomputing it on
// every request (or every 1s reconciler tick) is the hot path §10 targets.
// This package keeps the latest computed ProjectSnapshot per project in
// memory and serves it instantly, refreshing it in the background when it
// goes stale rather than blocking the reader.
//
// Nothing here is canonical: a ProjectSnapshot is a presentation-only
// cached view. On a refresh error the previous snapshot is retained (the
// UI keeps showing the last-known-good counts) rather than being cleared.
//
// The in-memory cache alone is only warm within one process: every
// `punakawan panel` restart starts empty, so the first read for each
// project still pays the full recompute. WithPersistence closes that gap -
// each successful refresh is durably saved, and a cold in-memory miss loads
// that saved value first, so a restart serves an instant (if briefly
// stale) read instead of blocking, and a background refresh brings it back
// to date.
package snapshot

import (
	"context"
	"sync"
	"time"
)

// ProjectSnapshot is a project's cached overview counts, mirroring
// punakawan-panel-project-performance-improvement-plan.md §10.1. All
// fields are presentation-only; none is a canonical source of truth.
type ProjectSnapshot struct {
	ProjectID          string    `json:"project_id"`
	UpdatedAt          time.Time `json:"updated_at"`
	Availability       string    `json:"availability"`
	RepositoryCount    int       `json:"repository_count"`
	KnowledgeCount     int       `json:"knowledge_count"`
	WorkflowCount      int       `json:"workflow_count"`
	PlanCount          int       `json:"plan_count"`
	ActiveRunCount     int       `json:"active_run_count"`
	OpenTaskCount      int       `json:"open_task_count"`
	BlockedTaskCount   int       `json:"blocked_task_count"`
	PendingReviewCount int       `json:"pending_review_count"`
}

// RefreshFunc recomputes a project's snapshot from its underlying sources.
// It is injected so the cache stays free of any source dependency and is
// fully unit-testable with a fake.
type RefreshFunc func(ctx context.Context, projectID string) (*ProjectSnapshot, error)

// LoadPersistedFunc loads a snapshot a previous process already computed
// and persisted for projectID, if any. ok is false when nothing is
// persisted (or persistence is unavailable), in which case the cache
// behaves exactly as it would with no persistence configured at all.
type LoadPersistedFunc func(projectID string) (snap *ProjectSnapshot, ok bool)

// SavePersistedFunc durably stores a freshly computed snapshot so a later
// process - a brand new `punakawan panel` invocation, which always starts
// with an empty in-memory cache - can seed itself from disk instead of
// recomputing from scratch. Persistence is best-effort: a failure here
// must never fail the refresh that produced snap.
type SavePersistedFunc func(snap *ProjectSnapshot)

// DefaultTTL is how long a cached snapshot is served as fresh before a
// background refresh is triggered on the next read.
const DefaultTTL = 10 * time.Second

// Cache is an in-memory store of the latest ProjectSnapshot per project,
// with stale-while-revalidate reads and per-project refresh dedup.
//
// The zero value is not usable; construct with New.
type Cache struct {
	ttl     time.Duration
	refresh RefreshFunc
	load    LoadPersistedFunc
	save    SavePersistedFunc
	now     func() time.Time

	mu   sync.RWMutex
	snap map[string]*ProjectSnapshot

	// flight deduplicates concurrent refreshes per projectID (an inline
	// minimal singleflight; see doRefresh).
	flightMu sync.Mutex
	flight   map[string]*call
}

// call is one in-progress refresh for a projectID.
type call struct {
	done chan struct{}
	snap *ProjectSnapshot
	err  error
}

// Option configures a Cache.
type Option func(*Cache)

// WithTTL sets the staleness threshold. A non-positive ttl leaves the
// default.
func WithTTL(ttl time.Duration) Option {
	return func(c *Cache) {
		if ttl > 0 {
			c.ttl = ttl
		}
	}
}

// WithClock injects a clock for deterministic tests. A nil now leaves the
// default (time.Now).
func WithClock(now func() time.Time) Option {
	return func(c *Cache) {
		if now != nil {
			c.now = now
		}
	}
}

// WithPersistence gives the cache a durable backing store: load seeds a
// cold (never-yet-cached-this-process) project from whatever an earlier
// process last saved, so a fresh `punakawan panel` restart serves an
// instant (if momentarily stale) read instead of blocking on a full
// recompute; save is called after every successful refresh so the next
// restart has something to load. Either func may be nil to disable that
// half (e.g. a read-only cache that never persists).
func WithPersistence(load LoadPersistedFunc, save SavePersistedFunc) Option {
	return func(c *Cache) {
		c.load = load
		c.save = save
	}
}

// New builds a Cache. refresh may be nil (GetOrRefresh then behaves like
// Get), though callers normally inject one.
func New(refresh RefreshFunc, opts ...Option) *Cache {
	c := &Cache{
		ttl:     DefaultTTL,
		refresh: refresh,
		now:     time.Now,
		snap:    make(map[string]*ProjectSnapshot),
		flight:  make(map[string]*call),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Get returns the cached snapshot for projectID, if any, without ever
// triggering a refresh. The returned pointer is a copy safe for the
// caller to read; the cache never mutates a snapshot after storing it.
func (c *Cache) Get(projectID string) (*ProjectSnapshot, bool) {
	c.mu.RLock()
	s, ok := c.snap[projectID]
	c.mu.RUnlock()
	return s, ok
}

// GetOrRefresh returns the cached snapshot immediately (stale-while-
// revalidate): if the snapshot is missing or older than the TTL it kicks
// off at most one background refresh for that projectID and returns what
// is currently cached. The second return value is true when the returned
// snapshot is stale or absent (i.e. a refresh was triggered), so callers
// can distinguish a served-fresh hit from a served-stale one.
//
// When nothing is cached yet in this process and no persisted snapshot can
// be loaded either, GetOrRefresh returns (nil, true) and the first
// successful refresh populates the cache for the next read - it never
// blocks the caller on the expensive recompute. When a persisted snapshot
// *is* available (WithPersistence), that seeds the in-memory cache first,
// so a cold process restart still returns instantly (serving a possibly
// stale value) instead of falling through to nil.
func (c *Cache) GetOrRefresh(ctx context.Context, projectID string) (*ProjectSnapshot, bool) {
	c.mu.RLock()
	s, ok := c.snap[projectID]
	c.mu.RUnlock()

	if !ok && c.load != nil {
		if loaded, loadOK := c.load(projectID); loadOK && loaded != nil {
			c.mu.Lock()
			if cur, already := c.snap[projectID]; already {
				s = cur
			} else {
				c.snap[projectID] = loaded
				s = loaded
			}
			c.mu.Unlock()
			ok = true
		}
	}

	stale := !ok || c.now().Sub(s.UpdatedAt) >= c.ttl
	if stale && c.refresh != nil {
		c.triggerRefresh(projectID)
	}
	return s, stale
}

// Set stores snap as the latest snapshot for its ProjectID, and persists it
// (WithPersistence) so a later process can load it back. UpdatedAt is
// stamped (from the cache clock) if the caller left it zero.
func (c *Cache) Set(snap *ProjectSnapshot) {
	if snap == nil {
		return
	}
	if snap.UpdatedAt.IsZero() {
		snap.UpdatedAt = c.now()
	}
	c.mu.Lock()
	c.snap[snap.ProjectID] = snap
	c.mu.Unlock()
	if c.save != nil {
		c.save(snap)
	}
}

// Invalidate drops any cached snapshot for projectID, forcing the next
// GetOrRefresh to treat it as missing (and refresh).
func (c *Cache) Invalidate(projectID string) {
	c.mu.Lock()
	delete(c.snap, projectID)
	c.mu.Unlock()
}

// triggerRefresh launches a background refresh unless one is already in
// flight for projectID.
func (c *Cache) triggerRefresh(projectID string) {
	c.flightMu.Lock()
	if _, running := c.flight[projectID]; running {
		c.flightMu.Unlock()
		return
	}
	cl := &call{done: make(chan struct{})}
	c.flight[projectID] = cl
	c.flightMu.Unlock()

	go c.doRefresh(projectID, cl)
}

// doRefresh runs one refresh for projectID and clears its flight entry.
// On success the new snapshot is stored; on error the previous snapshot is
// left untouched (presentation-only: the UI keeps last-known-good counts).
func (c *Cache) doRefresh(projectID string, cl *call) {
	defer func() {
		c.flightMu.Lock()
		delete(c.flight, projectID)
		c.flightMu.Unlock()
		close(cl.done)
	}()

	snap, err := c.refresh(context.Background(), projectID)
	cl.err = err
	if err != nil || snap == nil {
		return
	}
	snap.ProjectID = projectID
	cl.snap = snap
	c.Set(snap)
}

// Refresh synchronously refreshes projectID, deduplicating against any
// concurrent background refresh (all concurrent callers observe the same
// single underlying RefreshFunc invocation). It returns the refreshed
// snapshot, or the current cached one plus the error on failure. Primarily
// useful in tests and for eager warm-up; normal read paths use
// GetOrRefresh and never block.
func (c *Cache) Refresh(ctx context.Context, projectID string) (*ProjectSnapshot, error) {
	if c.refresh == nil {
		s, _ := c.Get(projectID)
		return s, nil
	}

	c.flightMu.Lock()
	if cl, running := c.flight[projectID]; running {
		c.flightMu.Unlock()
		<-cl.done
		if cl.err != nil {
			cur, _ := c.Get(projectID)
			return cur, cl.err
		}
		return cl.snap, nil
	}
	cl := &call{done: make(chan struct{})}
	c.flight[projectID] = cl
	c.flightMu.Unlock()

	c.doRefresh(projectID, cl)
	if cl.err != nil {
		cur, _ := c.Get(projectID)
		return cur, cl.err
	}
	return cl.snap, nil
}
