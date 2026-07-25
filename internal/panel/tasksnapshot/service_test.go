package tasksnapshot

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/beads"
)

// fixture builds a small, deterministic bd-shaped issue set plus its ready
// set. Board columns exercised:
//   - t1: open + ready        -> ready
//   - t2: open, blocked by t3 -> blocked (open, not ready, unmet "blocks")
//   - t3: in_progress         -> active
//   - t4: closed              -> completed
//   - t5: stored "blocked"    -> blocked
func fixture() ([]beads.ReadyIssue, map[string]bool) {
	now := time.Now().UTC().Format(time.RFC3339)
	issues := []beads.ReadyIssue{
		{ID: "t1", Title: "one", Status: "open", Priority: 1, IssueType: "task", UpdatedAt: now},
		{ID: "t2", Title: "two", Status: "open", Priority: 2, IssueType: "task", UpdatedAt: now,
			Dependencies: []beads.ReadyDependency{{IssueId: "t2", DependsOnId: "t3", Type: "blocks"}}},
		{ID: "t3", Title: "three", Status: "in_progress", Priority: 0, IssueType: "task", UpdatedAt: now},
		{ID: "t4", Title: "four", Status: "closed", Priority: 3, IssueType: "task", UpdatedAt: now},
		{ID: "t5", Title: "five", Status: "blocked", Priority: 2, IssueType: "bug", UpdatedAt: now},
	}
	ready := map[string]bool{"t1": true}
	return issues, ready
}

func TestRefreshBuildsCountsAndGraph(t *testing.T) {
	issues, ready := fixture()
	svc := NewService(func(ctx context.Context) ([]beads.ReadyIssue, map[string]bool, error) {
		return issues, ready, nil
	})

	snap, err := svc.Refresh(context.Background(), "proj")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if snap.ProjectID != "proj" {
		t.Fatalf("ProjectID = %q, want proj", snap.ProjectID)
	}
	if snap.Stale {
		t.Fatalf("fresh snapshot marked stale")
	}
	if len(snap.Tasks) != 5 {
		t.Fatalf("Tasks = %d, want 5", len(snap.Tasks))
	}

	if snap.OpenCount != 2 {
		t.Errorf("OpenCount = %d, want 2", snap.OpenCount)
	}
	if snap.ReadyCount != 1 {
		t.Errorf("ReadyCount = %d, want 1", snap.ReadyCount)
	}
	if snap.ActiveCount != 1 {
		t.Errorf("ActiveCount = %d, want 1", snap.ActiveCount)
	}
	if snap.BlockedCount != 2 {
		t.Errorf("BlockedCount = %d, want 2 (t2 open-not-ready + t5 blocked)", snap.BlockedCount)
	}
	if snap.CompletedCount != 1 {
		t.Errorf("CompletedCount = %d, want 1", snap.CompletedCount)
	}

	// One dependency edge (t2 blocks t3), no cycle.
	if len(snap.Graph.Edges) != 1 {
		t.Fatalf("Graph.Edges = %+v, want 1", snap.Graph.Edges)
	}
	e := snap.Graph.Edges[0]
	if e.From != "t2" || e.To != "t3" || e.Type != "blocks" {
		t.Errorf("edge = %+v, want t2->t3 blocks", e)
	}
	if len(snap.Graph.Cycles) != 0 {
		t.Errorf("Graph.Cycles = %+v, want none", snap.Graph.Cycles)
	}

	// t2's blocking reason should name its unmet "blocks" target (t3).
	found := false
	for _, task := range snap.Tasks {
		if task.ID != "t2" {
			continue
		}
		found = true
		if len(task.BlockingReasons) != 1 {
			t.Fatalf("t2 BlockingReasons = %+v, want 1", task.BlockingReasons)
		}
	}
	if !found {
		t.Fatalf("t2 not found in snapshot")
	}

	// Cached: Get returns the same snapshot without a new refresh.
	got, ok := svc.Get("proj")
	if !ok || got != snap {
		t.Fatalf("Get after Refresh = (%v, %v), want cached snapshot", got, ok)
	}
}

func TestConcurrentRefreshShared(t *testing.T) {
	issues, ready := fixture()
	var calls int32
	entered := make(chan struct{})
	release := make(chan struct{})
	svc := NewService(func(ctx context.Context) ([]beads.ReadyIssue, map[string]bool, error) {
		atomic.AddInt32(&calls, 1)
		entered <- struct{}{}
		<-release
		return issues, ready, nil
	})

	const n = 6
	var wg sync.WaitGroup
	snaps := make([]*ProjectTaskSnapshot, n)

	// First caller: registers the inflight entry and blocks inside runner.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s, err := svc.Refresh(context.Background(), "proj")
		if err != nil {
			t.Errorf("Refresh[0]: %v", err)
		}
		snaps[0] = s
	}()
	<-entered // runner is now executing; inflight["proj"] is set.

	// Followers: must join the inflight call rather than start their own.
	for i := 1; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := svc.Refresh(context.Background(), "proj")
			if err != nil {
				t.Errorf("Refresh[%d]: %v", i, err)
			}
			snaps[i] = s
		}(i)
	}
	// Give followers time to register on the inflight call before releasing.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("runner calls = %d, want 1 (singleflight)", got)
	}
	for i := 1; i < n; i++ {
		if snaps[i] != snaps[0] {
			t.Fatalf("snaps[%d] != snaps[0]; concurrent refresh not shared", i)
		}
	}
}

func TestRefreshFailureKeepsStaleSnapshot(t *testing.T) {
	issues, ready := fixture()
	var fail atomic.Bool
	sentinel := errors.New("bd unavailable")
	svc := NewService(func(ctx context.Context) ([]beads.ReadyIssue, map[string]bool, error) {
		if fail.Load() {
			return nil, nil, sentinel
		}
		return issues, ready, nil
	})

	first, err := svc.Refresh(context.Background(), "proj")
	if err != nil {
		t.Fatalf("initial Refresh: %v", err)
	}

	fail.Store(true)
	stale, err := svc.Refresh(context.Background(), "proj")
	if !errors.Is(err, sentinel) {
		t.Fatalf("failed Refresh err = %v, want sentinel", err)
	}
	if stale == nil {
		t.Fatalf("failed Refresh returned nil snapshot, want retained stale copy")
	}
	if !stale.Stale {
		t.Fatalf("retained snapshot not marked Stale")
	}
	if stale.OpenCount != first.OpenCount || len(stale.Tasks) != len(first.Tasks) {
		t.Fatalf("retained snapshot lost data: got open=%d tasks=%d", stale.OpenCount, len(stale.Tasks))
	}

	// The cached entry now reflects the stale copy.
	got, ok := svc.Get("proj")
	if !ok || !got.Stale {
		t.Fatalf("Get after failed Refresh = (stale=%v, ok=%v), want stale cached", got != nil && got.Stale, ok)
	}
}

func TestRefreshFailureWithNoPriorSnapshot(t *testing.T) {
	sentinel := errors.New("bd unavailable")
	svc := NewService(func(ctx context.Context) ([]beads.ReadyIssue, map[string]bool, error) {
		return nil, nil, sentinel
	})
	snap, err := svc.Refresh(context.Background(), "proj")
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if snap != nil {
		t.Fatalf("snap = %+v, want nil when no prior snapshot exists", snap)
	}
	if _, ok := svc.Get("proj"); ok {
		t.Fatalf("Get should report no cached snapshot after a first-time failure")
	}
}

func TestInvalidateForcesRefresh(t *testing.T) {
	issues, ready := fixture()
	var calls int32
	svc := NewService(func(ctx context.Context) ([]beads.ReadyIssue, map[string]bool, error) {
		atomic.AddInt32(&calls, 1)
		return issues, ready, nil
	})

	if _, err := svc.Refresh(context.Background(), "proj"); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if _, ok := svc.Get("proj"); !ok {
		t.Fatalf("expected cached snapshot after Refresh")
	}

	svc.Invalidate("proj")
	if _, ok := svc.Get("proj"); ok {
		t.Fatalf("Get after Invalidate should miss")
	}

	if _, err := svc.Refresh(context.Background(), "proj"); err != nil {
		t.Fatalf("Refresh after Invalidate: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("runner calls = %d, want 2 (invalidate forced a rebuild)", got)
	}
}

func TestBuildSnapshotDetectsCycle(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	issues := []beads.ReadyIssue{
		{ID: "a", Status: "open", UpdatedAt: now,
			Dependencies: []beads.ReadyDependency{{IssueId: "a", DependsOnId: "b", Type: "related"}}},
		{ID: "b", Status: "open", UpdatedAt: now,
			Dependencies: []beads.ReadyDependency{{IssueId: "b", DependsOnId: "a", Type: "related"}}},
	}
	snap := BuildSnapshot("proj", issues, map[string]bool{})
	if len(snap.Graph.Cycles) == 0 {
		t.Fatalf("expected a detected cycle in %+v", snap.Graph.Edges)
	}
}
