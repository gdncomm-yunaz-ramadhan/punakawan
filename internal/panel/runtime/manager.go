// Package runtime provides a bounded, shared pool of loaded *app.App
// instances keyed by project (workspace) id, per the panel implementation
// plan §10.3.
//
// Motivation: the panel's non-primary workspace reads (WorkspaceSource.Get
// / summaryFor, GlobalSearchSource.Search) previously did a fresh
// app.Load(path) + Close() on every request. app.Load discovers the
// workspace and wires up its per-project state (policy, git inspector,
// worktree manager, and the JSONL-backed workflow/PR-review/context-request
// stores); knowledge, tasks, approvals, learning, and the sync queue are
// thin scopes over one shared SQLite storage kernel rather than per-project
// stores, so they do not need this pooling. What genuinely benefits from
// staying warm across requests is workspace discovery itself and, above
// all, the per-project SQLite FTS5 search index, which is lazily memoized
// on the App behind a mutex (App.OpenSearchIndex). Reloading per request
// throws that memoization (and the discovery work) away and repays the cost
// every time. ProjectRuntimeManager keeps a bounded set of these Apps warm
// and hands them out under reference counting so a busy project is loaded
// once and reused.
//
// Bounding: at most maxActive runtimes are kept in the pool (the primary
// counts toward the cap). Admitting a new runtime beyond the cap evicts the
// least-recently-used runtime that is not the primary and has no
// outstanding in-use references, closing its App. If every non-primary
// runtime is currently in use, the pool is allowed to grow temporarily over
// the cap rather than block the caller - the over-cap runtimes become
// eviction candidates as soon as their callers release them.
//
// The primary App is a long-lived singleton owned by the panel command and
// shared via panel.Readers; it is seeded into the pool up front, marked
// primary, and is never evicted or closed by the manager.
package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ygrip/punakawan/internal/app"
)

// Default pool bounds. Both are overridable via options.
const (
	defaultMaxActive   = 4
	defaultIdleTimeout = 12 * time.Minute
)

// ProjectRuntime is a pooled, loaded workspace App plus the bookkeeping the
// manager needs to reference-count and age it out. All mutable fields are
// guarded by the owning manager's mutex; callers must not mutate them
// directly. App is the only field intended for use by consumers, and only
// while they hold an outstanding Acquire reference (i.e. before calling the
// returned release func).
type ProjectRuntime struct {
	App       *app.App
	ProjectID string
	Path      string

	// primary marks the singleton App owned by the panel command. A
	// primary runtime is never evicted, invalidated, or closed by the
	// manager.
	primary bool

	// lastUsedAt is bumped on every Acquire/Release, and drives CloseIdle's
	// idle-timeout decision. Guarded by the manager mutex.
	lastUsedAt time.Time

	// inUse counts outstanding Acquire references not yet released. A
	// runtime with inUse > 0 is never evicted or closed. Guarded by the
	// manager mutex.
	inUse int

	// closeOnRelease is set when Invalidate is called on a runtime that is
	// still in use: it cannot be closed immediately, so it is closed when
	// its last reference is released. Guarded by the manager mutex.
	closeOnRelease bool
}

// Manager is the interface the panel sources depend on. ProjectRuntimeManager
// is the only implementation; the interface exists to document the surface
// and to keep call sites swappable in tests.
type Manager interface {
	Acquire(ctx context.Context, projectID, path string) (*ProjectRuntime, func(), error)
	Release(projectID string)
	Invalidate(projectID string) error
	CloseIdle(ctx context.Context) error
	Close(ctx context.Context) error
	ActiveCount() int
	StopIdleTimer()
}

// scheduledTimer is the subset of *time.Timer the manager needs, so a test can
// inject a fake one and drive idle-expiry firing deterministically instead of
// waiting on real wall-clock time.
type scheduledTimer interface {
	Stop() bool
}

// ProjectRuntimeManager is a concurrency-safe, bounded pool of ProjectRuntime.
type ProjectRuntimeManager struct {
	mu   sync.Mutex
	pool map[string]*ProjectRuntime

	primaryID   string
	maxActive   int
	idleTimeout time.Duration

	// loader loads a fresh App for a workspace path. Defaults to app.Load;
	// injectable for tests so pool mechanics can be exercised without real
	// Apps.
	loader func(path string) (*app.App, error)
	// closer closes an App the manager is evicting/invalidating/shutting
	// down. Defaults to (*app.App).Close; injectable for tests.
	closer func(*app.App) error
	// now supplies the current time for LRU/idle bookkeeping. Defaults to
	// time.Now; injectable for deterministic tests.
	now func() time.Time
	// newTimer schedules fireIdleTimer after a duration. Defaults to
	// time.AfterFunc; injectable for tests so idle-expiry firing can be driven
	// without a real wait.
	newTimer func(d time.Duration, f func()) scheduledTimer

	// idleTimer is the single outstanding timer for the pool's nearest idle
	// expiry, replacing a fixed periodic sweep: it is (re)scheduled on every
	// Acquire/Release/SetIdleTimeout to the next non-primary, unused
	// runtime's deadline. Guarded by mu.
	idleTimer scheduledTimer
}

var _ Manager = (*ProjectRuntimeManager)(nil)

// Option configures a ProjectRuntimeManager at construction.
type Option func(*ProjectRuntimeManager)

// WithMaxActive sets the pool cap (including the primary). Values < 1 are
// ignored, leaving the default.
func WithMaxActive(n int) Option {
	return func(m *ProjectRuntimeManager) {
		if n >= 1 {
			m.maxActive = n
		}
	}
}

// WithIdleTimeout sets how long a non-primary, unused runtime may sit idle
// before CloseIdle reclaims it. Values <= 0 are ignored.
func WithIdleTimeout(d time.Duration) Option {
	return func(m *ProjectRuntimeManager) {
		if d > 0 {
			m.idleTimeout = d
		}
	}
}

// WithLoader overrides the App loader (default app.Load). Intended for tests.
func WithLoader(fn func(path string) (*app.App, error)) Option {
	return func(m *ProjectRuntimeManager) {
		if fn != nil {
			m.loader = fn
		}
	}
}

// WithCloser overrides the App closer (default (*app.App).Close). Intended
// for tests.
func WithCloser(fn func(*app.App) error) Option {
	return func(m *ProjectRuntimeManager) {
		if fn != nil {
			m.closer = fn
		}
	}
}

// WithClock overrides the time source (default time.Now). Intended for tests.
func WithClock(fn func() time.Time) Option {
	return func(m *ProjectRuntimeManager) {
		if fn != nil {
			m.now = fn
		}
	}
}

// withTimerFactory overrides how the idle-expiry timer is scheduled (default
// time.AfterFunc). Unexported: only this package's own tests need to fire
// idle expiry deterministically instead of waiting on real wall-clock time.
func withTimerFactory(fn func(d time.Duration, f func()) scheduledTimer) Option {
	return func(m *ProjectRuntimeManager) {
		if fn != nil {
			m.newTimer = fn
		}
	}
}

// NewManager builds a manager seeded with the primary App. primaryApp may be
// nil (e.g. in a test that only exercises pool mechanics), in which case no
// primary is seeded. The primary is registered under primaryID and is never
// evicted or closed by the manager.
func NewManager(primaryID string, primaryApp *app.App, opts ...Option) *ProjectRuntimeManager {
	m := &ProjectRuntimeManager{
		pool:        make(map[string]*ProjectRuntime),
		primaryID:   primaryID,
		maxActive:   defaultMaxActive,
		idleTimeout: defaultIdleTimeout,
		loader:      func(path string) (*app.App, error) { return app.Load(path) },
		closer:      func(a *app.App) error { return a.Close() },
		now:         time.Now,
		newTimer: func(d time.Duration, f func()) scheduledTimer {
			return time.AfterFunc(d, f)
		},
	}
	for _, o := range opts {
		o(m)
	}
	if primaryApp != nil {
		m.pool[primaryID] = &ProjectRuntime{
			App:        primaryApp,
			ProjectID:  primaryID,
			primary:    true,
			lastUsedAt: m.now(),
			inUse:      0,
		}
	}
	return m
}

// Acquire returns a pooled runtime for projectID, loading and admitting one
// if the pool has none. The returned release func must be called exactly
// once when the caller is done; it decrements the in-use count (it does NOT
// close the App). release is idempotent - extra calls are no-ops.
//
// Admitting a new runtime that would exceed the cap first evicts the LRU
// idle, non-primary runtime. If none is evictable (all in use), the pool is
// allowed to grow past the cap temporarily.
func (m *ProjectRuntimeManager) Acquire(ctx context.Context, projectID, path string) (*ProjectRuntime, func(), error) {
	m.mu.Lock()
	if rt, ok := m.pool[projectID]; ok {
		rt.inUse++
		rt.lastUsedAt = m.now()
		m.scheduleIdleTimerLocked()
		m.mu.Unlock()
		return rt, m.releaseFunc(projectID), nil
	}
	m.mu.Unlock()

	// Load outside the lock: app.Load does filesystem discovery and store
	// opens, and must not block other projects' Acquire/Release.
	a, err := m.loader(path)
	if err != nil {
		return nil, nil, err
	}

	m.mu.Lock()
	// Another goroutine may have admitted the same projectID while we were
	// loading. If so, reuse theirs and discard our redundant load.
	if rt, ok := m.pool[projectID]; ok {
		rt.inUse++
		rt.lastUsedAt = m.now()
		m.scheduleIdleTimerLocked()
		m.mu.Unlock()
		_ = m.closer(a)
		return rt, m.releaseFunc(projectID), nil
	}

	rt := &ProjectRuntime{
		App:        a,
		ProjectID:  projectID,
		Path:       path,
		lastUsedAt: m.now(),
		inUse:      1,
	}
	m.pool[projectID] = rt
	evicted := m.selectEvictionsLocked()
	m.scheduleIdleTimerLocked()
	m.mu.Unlock()

	for _, e := range evicted {
		_ = m.closer(e.App)
	}
	return rt, m.releaseFunc(projectID), nil
}

// ActiveNonPrimaryIDs returns the project ids currently pooled (loaded),
// excluding the primary. It never Acquires or loads anything - it is a
// cheap snapshot for callers that want to piggyback on whichever projects
// are already warm (e.g. the tier-1 reconciler polling non-primary
// approvals) without forcing a cold project into the pool or evicting
// another one just to poll it.
func (m *ProjectRuntimeManager) ActiveNonPrimaryIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.pool))
	for id, rt := range m.pool {
		if !rt.primary {
			ids = append(ids, id)
		}
	}
	return ids
}

// MaxActive returns the current pool cap (including the primary).
func (m *ProjectRuntimeManager) MaxActive() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxActive
}

// SetMaxActive changes the pool cap at runtime (configurable via the system
// panel). Values < 1 are ignored. Lowering the cap immediately evicts the LRU
// idle, non-primary runtimes now over the new cap, freeing resources at once
// rather than only on the next admission. Runtimes still in use are never
// evicted; the pool may stay temporarily over cap until they are released.
func (m *ProjectRuntimeManager) SetMaxActive(n int) {
	if n < 1 {
		return
	}
	m.mu.Lock()
	m.maxActive = n
	evicted := m.selectEvictionsLocked()
	m.scheduleIdleTimerLocked()
	m.mu.Unlock()
	for _, e := range evicted {
		_ = m.closer(e.App)
	}
}

// IdleTimeout returns the current idle-shutdown timeout.
func (m *ProjectRuntimeManager) IdleTimeout() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.idleTimeout
}

// SetIdleTimeout changes how long a non-primary, unused runtime may sit idle
// before the periodic CloseIdle sweep reclaims it. Values <= 0 are ignored.
func (m *ProjectRuntimeManager) SetIdleTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	m.mu.Lock()
	m.idleTimeout = d
	m.scheduleIdleTimerLocked()
	m.mu.Unlock()
}

// ActiveCount returns the number of runtimes currently pooled, including the
// primary.
func (m *ProjectRuntimeManager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pool)
}

// releaseFunc returns an idempotent release closure bound to projectID.
func (m *ProjectRuntimeManager) releaseFunc(projectID string) func() {
	var once sync.Once
	return func() {
		once.Do(func() { m.Release(projectID) })
	}
}

// Release decrements the in-use count for projectID. Once that drops to
// zero, a runtime marked close-on-release (via Invalidate while in use) is
// dropped from the pool immediately; either way, selectEvictionsLocked then
// runs so a pool sitting over cap only because this runtime was busy is
// brought back within maxActive at once rather than waiting for the next
// Acquire. Every closed App is closed after the lock is released. Callers
// normally use the release func returned by Acquire rather than calling this
// directly.
func (m *ProjectRuntimeManager) Release(projectID string) {
	m.mu.Lock()
	rt, ok := m.pool[projectID]
	if !ok {
		m.mu.Unlock()
		return
	}
	if rt.inUse > 0 {
		rt.inUse--
	}
	rt.lastUsedAt = m.now()

	var evicted []*ProjectRuntime
	if rt.inUse == 0 {
		if rt.closeOnRelease && !rt.primary {
			delete(m.pool, projectID)
			evicted = append(evicted, rt)
		}
		evicted = append(evicted, m.selectEvictionsLocked()...)
	}
	m.scheduleIdleTimerLocked()
	m.mu.Unlock()

	for _, e := range evicted {
		_ = m.closer(e.App)
	}
}

// Invalidate drops and closes the pooled runtime for projectID (e.g. after
// its workspace's on-disk state changed and the cached App is stale). The
// primary is never invalidated. If the runtime is currently in use it is
// marked for close-on-release instead of being closed out from under its
// callers.
func (m *ProjectRuntimeManager) Invalidate(projectID string) error {
	m.mu.Lock()
	rt, ok := m.pool[projectID]
	if !ok || rt.primary {
		m.mu.Unlock()
		return nil
	}
	if rt.inUse > 0 {
		rt.closeOnRelease = true
		m.mu.Unlock()
		return nil
	}
	delete(m.pool, projectID)
	m.scheduleIdleTimerLocked()
	m.mu.Unlock()
	return m.closer(rt.App)
}

// CloseIdle closes every non-primary runtime that has no outstanding
// references and has been idle for at least idleTimeout, returning the
// aggregated close errors.
func (m *ProjectRuntimeManager) CloseIdle(ctx context.Context) error {
	now := m.now()
	m.mu.Lock()
	var toClose []*ProjectRuntime
	for id, rt := range m.pool {
		if rt.primary || rt.inUse > 0 {
			continue
		}
		if now.Sub(rt.lastUsedAt) >= m.idleTimeout {
			toClose = append(toClose, rt)
			delete(m.pool, id)
		}
	}
	m.scheduleIdleTimerLocked()
	m.mu.Unlock()

	var errs []error
	for _, rt := range toClose {
		if err := m.closer(rt.App); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Close closes all non-primary runtimes, for panel shutdown. The primary is
// left untouched: it is owned by the panel command, which closes it
// separately. In-use runtimes are closed too, since Close runs only at
// shutdown when no further reads are expected. It does not stop the idle
// timer itself - call StopIdleTimer first, per the documented shutdown order.
func (m *ProjectRuntimeManager) Close(ctx context.Context) error {
	m.mu.Lock()
	var toClose []*ProjectRuntime
	for id, rt := range m.pool {
		if rt.primary {
			continue
		}
		toClose = append(toClose, rt)
		delete(m.pool, id)
	}
	m.mu.Unlock()

	var errs []error
	for _, rt := range toClose {
		if err := m.closer(rt.App); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// StopIdleTimer cancels any outstanding idle-expiry timer without touching
// the pool itself. Part of the documented shutdown order (stop the idle timer
// before closing non-primary runtimes), so a timer cannot fire mid-shutdown
// and race the explicit Close.
func (m *ProjectRuntimeManager) StopIdleTimer() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
}

// scheduleIdleTimerLocked (re)schedules the single outstanding idle-expiry
// timer for the nearest non-primary, unused runtime's deadline, replacing a
// fixed periodic sweep: at most one timer is ever pending, and it always
// targets the runtime that will next become eligible for CloseIdle. Must be
// called with m.mu held.
func (m *ProjectRuntimeManager) scheduleIdleTimerLocked() {
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
	var nearest time.Time
	found := false
	for _, rt := range m.pool {
		if rt.primary || rt.inUse > 0 {
			continue
		}
		deadline := rt.lastUsedAt.Add(m.idleTimeout)
		if !found || deadline.Before(nearest) {
			nearest = deadline
			found = true
		}
	}
	if !found {
		return
	}
	d := nearest.Sub(m.now())
	if d < 0 {
		d = 0
	}
	m.idleTimer = m.newTimer(d, m.fireIdleTimer)
}

// fireIdleTimer runs when the scheduled idle deadline arrives: CloseIdle
// closes whatever is now actually expired (re-checking idleTimeout against
// the live clock, not just the fired timer) and reschedules for whatever
// remains.
func (m *ProjectRuntimeManager) fireIdleTimer() {
	_ = m.CloseIdle(context.Background())
}

// selectEvictionsLocked removes LRU idle non-primary runtimes from the pool
// until it is back within maxActive, returning the removed runtimes for the
// caller to close outside the lock. If no eligible runtime remains (every
// non-primary is in use), it stops early, allowing the pool to stay
// temporarily over cap. Must be called with m.mu held.
func (m *ProjectRuntimeManager) selectEvictionsLocked() []*ProjectRuntime {
	var evicted []*ProjectRuntime
	for len(m.pool) > m.maxActive {
		var victim *ProjectRuntime
		var victimID string
		for id, rt := range m.pool {
			if rt.primary || rt.inUse > 0 {
				continue
			}
			if victim == nil || rt.lastUsedAt.Before(victim.lastUsedAt) {
				victim = rt
				victimID = id
			}
		}
		if victim == nil {
			// Nothing evictable: allow temporary over-cap.
			break
		}
		delete(m.pool, victimID)
		evicted = append(evicted, victim)
	}
	return evicted
}
