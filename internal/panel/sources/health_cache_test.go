package sources

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// countingHealthReader is a contract.WorkspaceReader that counts Get calls (the
// "expensive" deep probe HealthCache avoids repeating) and can be made to fail
// or block, to exercise the cache's error-preserve and dedup paths.
type countingHealthReader struct {
	mu      sync.Mutex
	calls   map[string]int
	detail  map[string]contract.WorkspaceDetail
	err     error
	block   chan struct{} // when non-nil, Get waits on it before returning
	entered chan struct{} // when non-nil, signals (once) that Get has begun
}

func (r *countingHealthReader) List(ctx context.Context) ([]contract.WorkspaceSummary, error) {
	return nil, nil
}

func (r *countingHealthReader) Get(ctx context.Context, id string) (contract.WorkspaceDetail, error) {
	r.mu.Lock()
	r.calls[id]++
	err := r.err
	block := r.block
	entered := r.entered
	detail := r.detail[id]
	r.mu.Unlock()

	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		<-block
	}
	if err != nil {
		return contract.WorkspaceDetail{}, err
	}
	return detail, nil
}

func (r *countingHealthReader) count(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[id]
}

func (r *countingHealthReader) setErr(err error) {
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
}

func healthDetail(id, source string) contract.WorkspaceDetail {
	return contract.WorkspaceDetail{
		WorkspaceSummary: contract.WorkspaceSummary{
			ID:           id,
			Availability: protocol.PanelSourceHealthAvailabilityAvailable,
		},
		Health: []protocol.PanelSourceHealth{{
			Source:       source,
			Availability: protocol.PanelSourceHealthAvailabilityAvailable,
			CheckedAt:    time.Now().UTC(),
		}},
	}
}

// testClock is a manually-advanced clock for deterministic staleness tests.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// waitForHealth polls cond until true or the deadline, failing otherwise. Used
// to observe asynchronous background refreshes.
func waitForHealth(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func TestHealthCacheServesFromCache(t *testing.T) {
	inner := &countingHealthReader{
		calls:  map[string]int{},
		detail: map[string]contract.WorkspaceDetail{"alpha": healthDetail("alpha", "git")},
	}
	clk := &testClock{t: time.Unix(1000, 0)}
	h := NewHealthCache(inner, time.Minute, WithHealthClock(clk.now))
	ctx := context.Background()

	first, stale, err := h.Health(ctx, "alpha")
	if err != nil {
		t.Fatalf("first Health: %v", err)
	}
	if stale {
		t.Fatal("cold Health returned stale=true, want false after synchronous refresh")
	}
	if len(first.Health) != 1 || first.Health[0].Source != "git" {
		t.Fatalf("Health detail = %+v, want one git source", first.Health)
	}
	if got := inner.count("alpha"); got != 1 {
		t.Fatalf("Get calls after cold Health = %d, want 1", got)
	}

	// A warm, non-stale hit must not re-run Get.
	if _, stale, err := h.Health(ctx, "alpha"); err != nil || stale {
		t.Fatalf("warm Health = (stale=%v, err=%v), want (false, nil)", stale, err)
	}
	if got := inner.count("alpha"); got != 1 {
		t.Fatalf("Get calls after warm Health = %d, want still 1", got)
	}
}

func TestHealthCacheStaleTriggersBackgroundRefresh(t *testing.T) {
	inner := &countingHealthReader{
		calls:  map[string]int{},
		detail: map[string]contract.WorkspaceDetail{"alpha": healthDetail("alpha", "git")},
	}
	clk := &testClock{t: time.Unix(1000, 0)}
	h := NewHealthCache(inner, 30*time.Second, WithHealthClock(clk.now))
	ctx := context.Background()

	if _, _, err := h.Health(ctx, "alpha"); err != nil {
		t.Fatalf("warm Health: %v", err)
	}
	if got := inner.count("alpha"); got != 1 {
		t.Fatalf("Get calls = %d, want 1", got)
	}

	// Age the entry past the TTL.
	clk.advance(31 * time.Second)

	_, stale, err := h.Health(ctx, "alpha")
	if err != nil {
		t.Fatalf("stale Health: %v", err)
	}
	if !stale {
		t.Fatal("aged Health returned stale=false, want true")
	}
	// The stale read returns immediately and revalidates in the background.
	waitForHealth(t, func() bool { return inner.count("alpha") == 2 })
}

func TestHealthCacheRefreshErrorKeepsLastGood(t *testing.T) {
	inner := &countingHealthReader{
		calls:  map[string]int{},
		detail: map[string]contract.WorkspaceDetail{"alpha": healthDetail("alpha", "git")},
	}
	clk := &testClock{t: time.Unix(1000, 0)}
	h := NewHealthCache(inner, time.Minute, WithHealthClock(clk.now))
	ctx := context.Background()

	if _, _, err := h.Health(ctx, "alpha"); err != nil {
		t.Fatalf("seed Health: %v", err)
	}

	inner.setErr(errors.New("boom"))
	detail, err := h.Refresh(ctx, "alpha")
	if err == nil {
		t.Fatal("Refresh err = nil, want error")
	}
	if len(detail.Health) != 1 || detail.Health[0].Source != "git" {
		t.Fatalf("on error returned %+v, want last-good git health", detail.Health)
	}

	// Cache still serves the last-good value without error.
	got, stale, err := h.Health(ctx, "alpha")
	if err != nil || stale {
		t.Fatalf("Health after failed refresh = (stale=%v, err=%v), want (false, nil)", stale, err)
	}
	if len(got.Health) != 1 || got.Health[0].Source != "git" {
		t.Fatalf("Health after failed refresh = %+v, want last-good", got.Health)
	}
}

func TestHealthCacheRefreshForcesRecompute(t *testing.T) {
	inner := &countingHealthReader{
		calls:  map[string]int{},
		detail: map[string]contract.WorkspaceDetail{"alpha": healthDetail("alpha", "git")},
	}
	clk := &testClock{t: time.Unix(1000, 0)}
	h := NewHealthCache(inner, time.Hour, WithHealthClock(clk.now))
	ctx := context.Background()

	if _, _, err := h.Health(ctx, "alpha"); err != nil {
		t.Fatalf("seed Health: %v", err)
	}
	if got := inner.count("alpha"); got != 1 {
		t.Fatalf("Get calls = %d, want 1", got)
	}

	// Refresh recomputes even though the cached entry is still fresh.
	if _, err := h.Refresh(ctx, "alpha"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := inner.count("alpha"); got != 2 {
		t.Fatalf("Get calls after Refresh = %d, want 2", got)
	}
}

func TestHealthCacheDedupsConcurrentRefresh(t *testing.T) {
	block := make(chan struct{})
	entered := make(chan struct{}, 1)
	inner := &countingHealthReader{
		calls:   map[string]int{},
		detail:  map[string]contract.WorkspaceDetail{"alpha": healthDetail("alpha", "git")},
		block:   block,
		entered: entered,
	}
	clk := &testClock{t: time.Unix(1000, 0)}
	h := NewHealthCache(inner, time.Minute, WithHealthClock(clk.now))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = h.Health(context.Background(), "alpha")
		}()
	}
	<-entered // one Get is now in flight, holding the singleflight slot
	close(block)
	wg.Wait()

	if got := inner.count("alpha"); got != 1 {
		t.Fatalf("Get calls = %d, want 1 (concurrent cold misses coalesced)", got)
	}
}

func TestHealthCacheInvalidate(t *testing.T) {
	inner := &countingHealthReader{
		calls:  map[string]int{},
		detail: map[string]contract.WorkspaceDetail{"alpha": healthDetail("alpha", "git")},
	}
	clk := &testClock{t: time.Unix(1000, 0)}
	h := NewHealthCache(inner, time.Hour, WithHealthClock(clk.now))
	ctx := context.Background()

	if _, _, err := h.Health(ctx, "alpha"); err != nil {
		t.Fatalf("seed Health: %v", err)
	}
	h.Invalidate("alpha")

	// After invalidation the next Health is a cold miss -> another Get.
	if _, _, err := h.Health(ctx, "alpha"); err != nil {
		t.Fatalf("Health after Invalidate: %v", err)
	}
	if got := inner.count("alpha"); got != 2 {
		t.Fatalf("Get calls after Invalidate = %d, want 2", got)
	}
}
