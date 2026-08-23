package snapshot

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond up to a short deadline, failing the test on timeout.
// Used to observe background-refresh effects without racy fixed sleeps.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func TestGetReturnsCachedWithoutRefresh(t *testing.T) {
	var calls int32
	c := New(func(ctx context.Context, id string) (*ProjectSnapshot, error) {
		atomic.AddInt32(&calls, 1)
		return &ProjectSnapshot{ProjectID: id}, nil
	})

	if _, ok := c.Get("p1"); ok {
		t.Fatal("Get on empty cache returned ok=true")
	}
	c.Set(&ProjectSnapshot{ProjectID: "p1", RepositoryCount: 3})
	got, ok := c.Get("p1")
	if !ok || got.RepositoryCount != 3 {
		t.Fatalf("Get = %+v ok=%v, want RepositoryCount=3", got, ok)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("Get triggered %d refreshes, want 0", n)
	}
}

func TestGetOrRefreshServesStaleAndRefreshesInBackground(t *testing.T) {
	var calls int32
	now := time.Unix(1000, 0)
	c := New(func(ctx context.Context, id string) (*ProjectSnapshot, error) {
		atomic.AddInt32(&calls, 1)
		return &ProjectSnapshot{ProjectID: id, RepositoryCount: 99}, nil
	}, WithTTL(10*time.Second), WithClock(func() time.Time { return now }))

	// Seed a snapshot that is already older than the TTL.
	c.Set(&ProjectSnapshot{ProjectID: "p1", UpdatedAt: now.Add(-30 * time.Second), RepositoryCount: 1})

	got, stale := c.GetOrRefresh(context.Background(), "p1")
	if !stale {
		t.Fatal("stale=false, want true for an aged snapshot")
	}
	if got == nil || got.RepositoryCount != 1 {
		t.Fatalf("GetOrRefresh returned %+v, want stale cached RepositoryCount=1", got)
	}
	// Background refresh eventually replaces the cached value.
	waitFor(t, func() bool {
		s, _ := c.Get("p1")
		return s != nil && s.RepositoryCount == 99
	})
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("refresh called %d times, want 1", n)
	}
}

func TestGetOrRefreshFreshSnapshotDoesNotRefresh(t *testing.T) {
	var calls int32
	now := time.Unix(1000, 0)
	c := New(func(ctx context.Context, id string) (*ProjectSnapshot, error) {
		atomic.AddInt32(&calls, 1)
		return &ProjectSnapshot{ProjectID: id}, nil
	}, WithTTL(10*time.Second), WithClock(func() time.Time { return now }))

	c.Set(&ProjectSnapshot{ProjectID: "p1", UpdatedAt: now.Add(-1 * time.Second)})
	_, stale := c.GetOrRefresh(context.Background(), "p1")
	if stale {
		t.Fatal("stale=true for a fresh snapshot")
	}
	// give any (erroneous) goroutine a chance
	time.Sleep(10 * time.Millisecond)
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("fresh snapshot triggered %d refreshes, want 0", n)
	}
}

func TestGetOrRefreshMissingReturnsNilStale(t *testing.T) {
	done := make(chan struct{})
	c := New(func(ctx context.Context, id string) (*ProjectSnapshot, error) {
		defer close(done)
		return &ProjectSnapshot{ProjectID: id, KnowledgeCount: 7}, nil
	})

	got, stale := c.GetOrRefresh(context.Background(), "p1")
	if got != nil || !stale {
		t.Fatalf("GetOrRefresh on empty = (%v, %v), want (nil, true)", got, stale)
	}
	<-done
	waitFor(t, func() bool {
		s, ok := c.Get("p1")
		return ok && s.KnowledgeCount == 7
	})
}

func TestConcurrentRefreshesDeduplicated(t *testing.T) {
	var calls int32
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	c := New(func(ctx context.Context, id string) (*ProjectSnapshot, error) {
		atomic.AddInt32(&calls, 1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // hold the refresh open so concurrent triggers collide
		return &ProjectSnapshot{ProjectID: id}, nil
	})

	// Trigger many concurrent refreshes of the same project while one is
	// held open; only a single underlying RefreshFunc call should happen.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.GetOrRefresh(context.Background(), "p1")
		}()
	}
	<-entered // ensure the first refresh is in flight
	// Fire more after the first is running.
	for i := 0; i < 20; i++ {
		c.GetOrRefresh(context.Background(), "p1")
	}
	close(release)
	wg.Wait()
	waitFor(t, func() bool {
		_, ok := c.Get("p1")
		return ok
	})
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("refresh called %d times, want 1 (deduplicated)", n)
	}
}

func TestRefreshErrorKeepsOldSnapshot(t *testing.T) {
	now := time.Unix(1000, 0)
	c := New(func(ctx context.Context, id string) (*ProjectSnapshot, error) {
		return nil, errors.New("boom")
	}, WithTTL(5*time.Second), WithClock(func() time.Time { return now }))

	c.Set(&ProjectSnapshot{ProjectID: "p1", UpdatedAt: now.Add(-1 * time.Hour), RepositoryCount: 42})

	got, err := c.Refresh(context.Background(), "p1")
	if err == nil {
		t.Fatal("Refresh err = nil, want error")
	}
	if got == nil || got.RepositoryCount != 42 {
		t.Fatalf("on error returned %+v, want old snapshot RepositoryCount=42", got)
	}
	// Cache still holds the old snapshot.
	cur, ok := c.Get("p1")
	if !ok || cur.RepositoryCount != 42 {
		t.Fatalf("cache after error = %+v, want old RepositoryCount=42", cur)
	}
}

func TestInvalidateForcesRefresh(t *testing.T) {
	var calls int32
	c := New(func(ctx context.Context, id string) (*ProjectSnapshot, error) {
		atomic.AddInt32(&calls, 1)
		return &ProjectSnapshot{ProjectID: id}, nil
	})
	c.Set(&ProjectSnapshot{ProjectID: "p1", UpdatedAt: time.Now()})
	if _, ok := c.Get("p1"); !ok {
		t.Fatal("expected cached snapshot before invalidate")
	}
	c.Invalidate("p1")
	if _, ok := c.Get("p1"); ok {
		t.Fatal("snapshot still present after Invalidate")
	}
	_, stale := c.GetOrRefresh(context.Background(), "p1")
	if !stale {
		t.Fatal("stale=false after invalidate, want true")
	}
	waitFor(t, func() bool { return atomic.LoadInt32(&calls) == 1 })
}

func TestSetStampsUpdatedAt(t *testing.T) {
	now := time.Unix(500, 0)
	c := New(nil, WithClock(func() time.Time { return now }))
	c.Set(&ProjectSnapshot{ProjectID: "p1"})
	got, _ := c.Get("p1")
	if !got.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v, want stamped %v", got.UpdatedAt, now)
	}
}

func TestRefreshNilFuncReturnsCached(t *testing.T) {
	c := New(nil)
	c.Set(&ProjectSnapshot{ProjectID: "p1", RepositoryCount: 5})
	got, err := c.Refresh(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Refresh err = %v, want nil", err)
	}
	if got == nil || got.RepositoryCount != 5 {
		t.Fatalf("Refresh = %+v, want cached RepositoryCount=5", got)
	}
}

// TestGetOrRefreshSeedsFromPersistedSnapshot guards the fix for overview
// being slow on every `punakawan panel` restart: a process-cold cache (no
// in-memory entry yet) must serve a persisted snapshot instantly rather
// than falling through to nil, while still triggering a background
// refresh to bring it back up to date.
func TestGetOrRefreshSeedsFromPersistedSnapshot(t *testing.T) {
	var calls int32
	persisted := &ProjectSnapshot{ProjectID: "p1", UpdatedAt: time.Unix(1000, 0), RepositoryCount: 7}
	var loadCalls int32
	c := New(
		func(ctx context.Context, id string) (*ProjectSnapshot, error) {
			atomic.AddInt32(&calls, 1)
			return &ProjectSnapshot{ProjectID: id, RepositoryCount: 99}, nil
		},
		WithPersistence(
			func(projectID string) (*ProjectSnapshot, bool) {
				atomic.AddInt32(&loadCalls, 1)
				if projectID != "p1" {
					return nil, false
				}
				return persisted, true
			},
			func(*ProjectSnapshot) {},
		),
	)

	got, stale := c.GetOrRefresh(context.Background(), "p1")
	if got == nil || got.RepositoryCount != 7 {
		t.Fatalf("GetOrRefresh = %+v, want persisted RepositoryCount=7", got)
	}
	if !stale {
		t.Fatal("stale=false, want true (persisted snapshot is old and still triggers a refresh)")
	}
	if n := atomic.LoadInt32(&loadCalls); n != 1 {
		t.Fatalf("load called %d times, want 1", n)
	}

	waitFor(t, func() bool {
		s, _ := c.Get("p1")
		return s != nil && s.RepositoryCount == 99
	})
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("refresh called %d times, want 1", n)
	}

	// A second GetOrRefresh must not consult load again: the in-memory
	// cache is now populated (first by the persisted seed, then by the
	// background refresh), so load is only ever needed once per process.
	c.GetOrRefresh(context.Background(), "p1")
	if n := atomic.LoadInt32(&loadCalls); n != 1 {
		t.Fatalf("load called %d times after a warm read, want still 1", n)
	}
}

// TestSetPersistsSnapshot guards that every successful store - whether
// from a background refresh or an eager Set (e.g. the project detail page
// warming the cache) - is handed to the configured SavePersistedFunc, so a
// later process can load it back.
func TestSetPersistsSnapshot(t *testing.T) {
	var saved []*ProjectSnapshot
	c := New(nil, WithPersistence(nil, func(snap *ProjectSnapshot) {
		saved = append(saved, snap)
	}))

	c.Set(&ProjectSnapshot{ProjectID: "p1", RepositoryCount: 3})
	if len(saved) != 1 || saved[0].RepositoryCount != 3 {
		t.Fatalf("saved = %+v, want one snapshot with RepositoryCount=3", saved)
	}
}

// TestDoRefreshPersistsSuccessfulSnapshot guards that a background/
// synchronous refresh's result is persisted, not just Set's direct
// callers.
func TestDoRefreshPersistsSuccessfulSnapshot(t *testing.T) {
	var saved []*ProjectSnapshot
	c := New(
		func(ctx context.Context, id string) (*ProjectSnapshot, error) {
			return &ProjectSnapshot{ProjectID: id, RepositoryCount: 5}, nil
		},
		WithPersistence(nil, func(snap *ProjectSnapshot) {
			saved = append(saved, snap)
		}),
	)

	if _, err := c.Refresh(context.Background(), "p1"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(saved) != 1 || saved[0].RepositoryCount != 5 {
		t.Fatalf("saved = %+v, want one snapshot with RepositoryCount=5", saved)
	}
}

// TestRefreshErrorDoesNotPersist guards that a failed refresh - which
// keeps the previous in-memory snapshot untouched - also does not
// overwrite the previously persisted one with nothing.
func TestRefreshErrorDoesNotPersist(t *testing.T) {
	saveCalls := 0
	c := New(
		func(ctx context.Context, id string) (*ProjectSnapshot, error) {
			return nil, errors.New("boom")
		},
		WithPersistence(nil, func(*ProjectSnapshot) { saveCalls++ }),
	)

	if _, err := c.Refresh(context.Background(), "p1"); err == nil {
		t.Fatal("Refresh err = nil, want error")
	}
	if saveCalls != 0 {
		t.Fatalf("save called %d times on a failed refresh, want 0", saveCalls)
	}
}
