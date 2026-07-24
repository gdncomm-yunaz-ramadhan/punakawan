package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
)

// fakeEnv provides an injected loader/closer/clock so the pool mechanics can
// be exercised without constructing real *app.App instances (app.Load needs a
// discoverable workspace on disk and starts real stores). The loader hands
// back a distinct sentinel *app.App per path; the closer records which
// sentinels were closed. A manual clock keeps LRU/idle decisions
// deterministic.
type fakeEnv struct {
	mu sync.Mutex

	loadCount map[string]int // path -> number of loads
	loaded    map[*app.App]string
	closed    []*app.App
	loadErr   map[string]error // path -> error to return from loader
	closeErr  map[*app.App]error
	now       time.Time
}

func newFakeEnv() *fakeEnv {
	return &fakeEnv{
		loadCount: map[string]int{},
		loaded:    map[*app.App]string{},
		loadErr:   map[string]error{},
		closeErr:  map[*app.App]error{},
		now:       time.Unix(0, 0).UTC(),
	}
}

func (f *fakeEnv) loader(path string) (*app.App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadCount[path]++
	if err := f.loadErr[path]; err != nil {
		return nil, err
	}
	a := &app.App{}
	f.loaded[a] = path
	return a, nil
}

func (f *fakeEnv) closer(a *app.App) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, a)
	return f.closeErr[a]
}

func (f *fakeEnv) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeEnv) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *fakeEnv) loads(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loadCount[path]
}

func (f *fakeEnv) closedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.closed)
}

func (f *fakeEnv) wasClosed(a *app.App) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.closed {
		if c == a {
			return true
		}
	}
	return false
}

func (f *fakeEnv) manager(opts ...Option) *ProjectRuntimeManager {
	base := []Option{WithLoader(f.loader), WithCloser(f.closer), WithClock(f.clock)}
	return NewManager("primary", &app.App{}, append(base, opts...)...)
}

func TestAcquireReusesPooledRuntime(t *testing.T) {
	f := newFakeEnv()
	m := f.manager()
	ctx := context.Background()

	rt1, rel1, err := m.Acquire(ctx, "a", "/path/a")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	rel1()

	rt2, rel2, err := m.Acquire(ctx, "a", "/path/a")
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	rel2()

	if got := f.loads("/path/a"); got != 1 {
		t.Fatalf("expected 1 load for reused project, got %d", got)
	}
	if rt1 != rt2 {
		t.Fatalf("expected the same pooled runtime on reuse")
	}
	if rt1.App != rt2.App {
		t.Fatalf("expected the same App on reuse")
	}
}

func TestAcquirePrimaryDoesNotLoad(t *testing.T) {
	f := newFakeEnv()
	m := f.manager()
	ctx := context.Background()

	rt, rel, err := m.Acquire(ctx, "primary", "/ignored")
	if err != nil {
		t.Fatalf("acquire primary: %v", err)
	}
	defer rel()

	if got := f.loads("/ignored"); got != 0 {
		t.Fatalf("primary must not be loaded, got %d loads", got)
	}
	if !rt.primary {
		t.Fatalf("expected primary runtime to be marked primary")
	}
}

func TestLRUEvictionAtCapClosesLeastRecentlyUsed(t *testing.T) {
	f := newFakeEnv()
	// Cap of 3 including primary => 2 non-primary slots.
	m := f.manager(WithMaxActive(3))
	ctx := context.Background()

	// Acquire+release a and b at distinct times so LRU order is well defined.
	_, relA, _ := m.Acquire(ctx, "a", "/a")
	appA := m.pool["a"].App
	relA()
	f.advance(time.Minute)

	_, relB, _ := m.Acquire(ctx, "b", "/b")
	appB := m.pool["b"].App
	relB()
	f.advance(time.Minute)

	// Touch a so b becomes the least-recently-used.
	_, relA2, _ := m.Acquire(ctx, "a", "/a")
	relA2()
	f.advance(time.Minute)

	// Admitting c exceeds the cap (primary + a + b + c = 4 > 3) => evict LRU
	// non-primary, which is b.
	_, relC, _ := m.Acquire(ctx, "c", "/c")
	defer relC()

	if !f.wasClosed(appB) {
		t.Fatalf("expected LRU runtime b to be evicted and closed")
	}
	if f.wasClosed(appA) {
		t.Fatalf("recently-used runtime a must not be evicted")
	}
	if _, ok := m.pool["b"]; ok {
		t.Fatalf("evicted runtime b must be removed from pool")
	}
	if _, ok := m.pool["a"]; !ok {
		t.Fatalf("runtime a must remain in pool")
	}
}

func TestPrimaryNeverEvictedOrClosed(t *testing.T) {
	f := newFakeEnv()
	primary := &app.App{}
	m := NewManager("primary", primary,
		WithLoader(f.loader), WithCloser(f.closer), WithClock(f.clock),
		WithMaxActive(2)) // primary + 1 slot
	ctx := context.Background()

	// Fill well past the cap; primary must survive every eviction pass.
	for _, id := range []string{"a", "b", "c", "d"} {
		_, rel, err := m.Acquire(ctx, id, "/"+id)
		if err != nil {
			t.Fatalf("acquire %s: %v", id, err)
		}
		rel()
		f.advance(time.Minute)
	}

	if f.wasClosed(primary) {
		t.Fatalf("primary App must never be closed")
	}
	if _, ok := m.pool["primary"]; !ok {
		t.Fatalf("primary must never be evicted from pool")
	}

	// Close (shutdown) must also leave the primary alone.
	if err := m.Close(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if f.wasClosed(primary) {
		t.Fatalf("primary App must never be closed, even on shutdown")
	}
	if _, ok := m.pool["primary"]; !ok {
		t.Fatalf("primary must remain after Close")
	}
}

func TestEvictionSkipsInUseAllowsOverCap(t *testing.T) {
	f := newFakeEnv()
	m := f.manager(WithMaxActive(2)) // primary + 1 slot
	ctx := context.Background()

	// Hold a and b concurrently (both in use) with a 1-slot cap => the pool
	// is allowed to grow over cap rather than evict an in-use runtime.
	_, relA, _ := m.Acquire(ctx, "a", "/a")
	_, relB, _ := m.Acquire(ctx, "b", "/b")

	if f.closedCount() != 0 {
		t.Fatalf("no in-use runtime may be evicted, got %d closes", f.closedCount())
	}
	if len(m.pool) != 3 { // primary + a + b
		t.Fatalf("expected temporary over-cap pool of 3, got %d", len(m.pool))
	}

	relA()
	relB()
}

func TestCloseIdleRespectsTimeoutAndInUse(t *testing.T) {
	f := newFakeEnv()
	m := f.manager(WithMaxActive(10), WithIdleTimeout(12*time.Minute))
	ctx := context.Background()

	// idleOld: released, then aged past the timeout.
	_, relOld, _ := m.Acquire(ctx, "old", "/old")
	appOld := m.pool["old"].App
	relOld()

	// idleFresh: released but not aged.
	_, relFresh, _ := m.Acquire(ctx, "fresh", "/fresh")
	appFresh := m.pool["fresh"].App

	// busy: still in use.
	_, relBusy, _ := m.Acquire(ctx, "busy", "/busy")
	appBusy := m.pool["busy"].App

	// Age "old" past the timeout, keep "fresh" recent by touching it.
	f.advance(13 * time.Minute)
	relFresh() // bumps fresh.lastUsedAt to now, so it is not idle

	if err := m.CloseIdle(ctx); err != nil {
		t.Fatalf("close idle: %v", err)
	}

	if !f.wasClosed(appOld) {
		t.Fatalf("idle-past-timeout runtime must be closed")
	}
	if f.wasClosed(appFresh) {
		t.Fatalf("recently-used runtime must not be closed by CloseIdle")
	}
	if f.wasClosed(appBusy) {
		t.Fatalf("in-use runtime must not be closed by CloseIdle")
	}
	if _, ok := m.pool["old"]; ok {
		t.Fatalf("closed idle runtime must be removed from pool")
	}

	relBusy()
}

func TestReleaseAccountingAndSharedRefs(t *testing.T) {
	f := newFakeEnv()
	m := f.manager()
	ctx := context.Background()

	_, rel1, _ := m.Acquire(ctx, "a", "/a")
	_, rel2, _ := m.Acquire(ctx, "a", "/a")

	if got := m.pool["a"].inUse; got != 2 {
		t.Fatalf("expected inUse=2 with two references, got %d", got)
	}
	if got := f.loads("/a"); got != 1 {
		t.Fatalf("second concurrent acquire must reuse, got %d loads", got)
	}

	rel1()
	if got := m.pool["a"].inUse; got != 1 {
		t.Fatalf("expected inUse=1 after one release, got %d", got)
	}

	// Idempotent: extra calls to the same release func are no-ops.
	rel1()
	if got := m.pool["a"].inUse; got != 1 {
		t.Fatalf("release func must be idempotent, inUse=%d", got)
	}

	rel2()
	if got := m.pool["a"].inUse; got != 0 {
		t.Fatalf("expected inUse=0 after all releases, got %d", got)
	}
}

func TestInvalidateInUseDefersClose(t *testing.T) {
	f := newFakeEnv()
	m := f.manager()
	ctx := context.Background()

	_, rel, _ := m.Acquire(ctx, "a", "/a")
	appA := m.pool["a"].App

	if err := m.Invalidate("a"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	// Still in use => not closed yet, but marked.
	if f.wasClosed(appA) {
		t.Fatalf("in-use runtime must not be closed immediately on Invalidate")
	}
	if !m.pool["a"].closeOnRelease {
		t.Fatalf("in-use invalidated runtime must be marked close-on-release")
	}

	rel() // last reference dropped => close now
	if !f.wasClosed(appA) {
		t.Fatalf("invalidated runtime must be closed when last reference releases")
	}
	if _, ok := m.pool["a"]; ok {
		t.Fatalf("invalidated+released runtime must be removed from pool")
	}
}

func TestInvalidateIdleClosesImmediately(t *testing.T) {
	f := newFakeEnv()
	m := f.manager()
	ctx := context.Background()

	_, rel, _ := m.Acquire(ctx, "a", "/a")
	appA := m.pool["a"].App
	rel()

	if err := m.Invalidate("a"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if !f.wasClosed(appA) {
		t.Fatalf("idle invalidated runtime must be closed immediately")
	}
	if _, ok := m.pool["a"]; ok {
		t.Fatalf("invalidated runtime must be removed from pool")
	}
}

func TestInvalidatePrimaryIsNoOp(t *testing.T) {
	f := newFakeEnv()
	primary := &app.App{}
	m := NewManager("primary", primary, WithLoader(f.loader), WithCloser(f.closer), WithClock(f.clock))

	if err := m.Invalidate("primary"); err != nil {
		t.Fatalf("invalidate primary: %v", err)
	}
	if f.wasClosed(primary) {
		t.Fatalf("primary must never be closed via Invalidate")
	}
	if _, ok := m.pool["primary"]; !ok {
		t.Fatalf("primary must remain in pool after Invalidate")
	}
}

func TestCloseClosesNonPrimaryAndAggregatesErrors(t *testing.T) {
	f := newFakeEnv()
	m := f.manager()
	ctx := context.Background()

	_, relA, _ := m.Acquire(ctx, "a", "/a")
	appA := m.pool["a"].App
	relA()
	_, relB, _ := m.Acquire(ctx, "b", "/b")
	appB := m.pool["b"].App
	relB()

	sentinel := errors.New("boom")
	f.mu.Lock()
	f.closeErr[appB] = sentinel
	f.mu.Unlock()

	err := m.Close(ctx)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected aggregated close error to contain sentinel, got %v", err)
	}
	if !f.wasClosed(appA) || !f.wasClosed(appB) {
		t.Fatalf("Close must close all non-primary runtimes")
	}
	if _, ok := m.pool["a"]; ok {
		t.Fatalf("Close must clear non-primary runtimes from pool")
	}
}

func TestLoaderErrorSurfaces(t *testing.T) {
	f := newFakeEnv()
	sentinel := errors.New("load failed")
	f.loadErr["/bad"] = sentinel
	m := f.manager()
	ctx := context.Background()

	rt, rel, err := m.Acquire(ctx, "bad", "/bad")
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected loader error to surface, got %v", err)
	}
	if rt != nil || rel != nil {
		t.Fatalf("expected nil runtime/release on load error")
	}
	if _, ok := m.pool["bad"]; ok {
		t.Fatalf("failed load must not admit a pool entry")
	}
}
