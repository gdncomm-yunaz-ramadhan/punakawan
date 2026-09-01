package sources

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/snapshot"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// mkdir creates dir under t.TempDir and returns it, for registry entries
// whose path registry.Register requires to exist on disk.
func mkdir(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	return p
}

// fakeWorkspaceReader is a counting contract.WorkspaceReader stand-in: every
// Get is the "expensive" deep inspection the cache is meant to avoid
// repeating.
type fakeWorkspaceReader struct {
	mu       sync.Mutex
	getCalls map[string]int
	detail   map[string]contract.WorkspaceDetail
	// gate, when non-nil, blocks every Get until it is closed. It lets a
	// test hold a background refresh at the door so an assertion about
	// whether the deep Get has run yet is deterministic rather than racing
	// the refresh goroutine.
	gate chan struct{}
}

func (f *fakeWorkspaceReader) List(ctx context.Context) ([]contract.WorkspaceSummary, error) {
	out := make([]contract.WorkspaceSummary, 0, len(f.detail))
	for _, d := range f.detail {
		out = append(out, d.WorkspaceSummary)
	}
	return out, nil
}

func (f *fakeWorkspaceReader) Get(ctx context.Context, id string) (contract.WorkspaceDetail, error) {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	f.getCalls[id]++
	f.mu.Unlock()
	return f.detail[id], nil
}

func (f *fakeWorkspaceReader) calls(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getCalls[id]
}

func summaryFixture(id string, repos int) contract.WorkspaceDetail {
	return contract.WorkspaceDetail{WorkspaceSummary: contract.WorkspaceSummary{
		ID:              id,
		Availability:    protocol.PanelSourceHealthAvailabilityAvailable,
		RepositoryCount: repos,
		LastActivityAt:  time.Now().UTC(),
	}}
}

func TestCachedWorkspaceReaderServesFromCache(t *testing.T) {
	inner := &fakeWorkspaceReader{
		getCalls: map[string]int{},
		detail: map[string]contract.WorkspaceDetail{
			"alpha": summaryFixture("alpha", 42),
			"beta":  summaryFixture("beta", 7),
		},
	}
	reg := openTestRegistry(t)
	now := time.Now().UTC()
	if _, err := reg.Register("alpha", mkdir(t, "alpha"), "Alpha", now); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	if _, err := reg.Register("beta", mkdir(t, "beta"), "", now); err != nil {
		t.Fatalf("register beta: %v", err)
	}

	// Long TTL so nothing goes stale within the test.
	c := NewCachedWorkspaceReader(inner, reg, "alpha", time.Hour)
	ctx := context.Background()

	first, err := c.List(ctx)
	if err != nil {
		t.Fatalf("first List: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("first List returned %d entries, want 2", len(first))
	}
	byID := map[string]contract.WorkspaceSummary{}
	for _, s := range first {
		byID[s.ID] = s
	}
	if byID["alpha"].RepositoryCount != 42 {
		t.Errorf("alpha counts not served from snapshot: %+v", byID["alpha"])
	}
	if byID["alpha"].DisplayName != "Alpha" {
		t.Errorf("alpha DisplayName = %q, want registry override %q", byID["alpha"].DisplayName, "Alpha")
	}
	if !byID["alpha"].Primary {
		t.Error("alpha should be marked Primary")
	}
	if byID["beta"].Primary {
		t.Error("beta should not be Primary")
	}

	// Cold miss should have computed each project exactly once.
	if got := inner.calls("alpha"); got != 1 {
		t.Errorf("alpha Get calls after first List = %d, want 1", got)
	}

	// A warm List must not re-run the deep Get.
	if _, err := c.List(ctx); err != nil {
		t.Fatalf("second List: %v", err)
	}
	if got := inner.calls("alpha"); got != 1 {
		t.Errorf("alpha Get calls after warm List = %d, want still 1", got)
	}
	if got := inner.calls("beta"); got != 1 {
		t.Errorf("beta Get calls after warm List = %d, want still 1", got)
	}
}

func TestCachedWorkspaceReaderGetWarmsCache(t *testing.T) {
	inner := &fakeWorkspaceReader{
		getCalls: map[string]int{},
		detail:   map[string]contract.WorkspaceDetail{"alpha": summaryFixture("alpha", 5)},
	}
	reg := openTestRegistry(t)
	if _, err := reg.Register("alpha", mkdir(t, "alpha"), "", time.Now().UTC()); err != nil {
		t.Fatalf("register alpha: %v", err)
	}

	c := NewCachedWorkspaceReader(inner, reg, "alpha", time.Hour)
	ctx := context.Background()

	if _, err := c.Get(ctx, "alpha"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := inner.calls("alpha"); got != 1 {
		t.Fatalf("Get calls = %d, want 1", got)
	}

	// Get warmed the cache, so a following List should not trigger another Get.
	if _, err := c.List(ctx); err != nil {
		t.Fatalf("List after Get: %v", err)
	}
	if got := inner.calls("alpha"); got != 1 {
		t.Errorf("Get calls after List = %d, want still 1 (List served from warmed cache)", got)
	}
}

// TestCachedWorkspaceReaderPersistsSnapshotAcrossRestarts guards the fix
// for overview being slow on every `punakawan panel` restart: a brand new
// CachedWorkspaceReader (an empty in-memory cache, exactly what a fresh
// process starts with) must serve alpha's already-computed snapshot from
// the file the first instance persisted, instead of recomputing it via
// another expensive inner.Get.
func TestCachedWorkspaceReaderPersistsSnapshotAcrossRestarts(t *testing.T) {
	inner := &fakeWorkspaceReader{
		getCalls: map[string]int{},
		detail:   map[string]contract.WorkspaceDetail{"alpha": summaryFixture("alpha", 42)},
	}
	reg := openTestRegistry(t)
	dir := mkdir(t, "alpha")
	if _, err := reg.Register("alpha", dir, "", time.Now().UTC()); err != nil {
		t.Fatalf("register alpha: %v", err)
	}

	first := NewCachedWorkspaceReader(inner, reg, "alpha", time.Hour)
	ctx := context.Background()
	if _, err := first.List(ctx); err != nil {
		t.Fatalf("first process's List: %v", err)
	}
	if got := inner.calls("alpha"); got != 1 {
		t.Fatalf("alpha Get calls after first process's List = %d, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".punakawan", snapshotFileName)); err != nil {
		t.Fatalf("expected a persisted snapshot file: %v", err)
	}

	restarted := NewCachedWorkspaceReader(inner, reg, "alpha", time.Hour)
	out, err := restarted.List(ctx)
	if err != nil {
		t.Fatalf("restarted List: %v", err)
	}
	if len(out) != 1 || out[0].RepositoryCount != 42 {
		t.Fatalf("restarted List = %+v, want RepositoryCount=42 served from the persisted snapshot", out)
	}
	if got := inner.calls("alpha"); got != 1 {
		t.Fatalf("alpha Get calls after restart's List = %d, want still 1 (served from disk, not recomputed)", got)
	}
}

// TestCachedWorkspaceReaderRestartServesStalePersistedSnapshotWithoutBlocking
// covers the other half: even a persisted snapshot old enough to be stale
// under the configured TTL must still be served immediately (matching the
// warm-cache stale-while-revalidate behavior), with the live recompute
// happening in the background rather than blocking this List call.
func TestCachedWorkspaceReaderRestartServesStalePersistedSnapshotWithoutBlocking(t *testing.T) {
	dir := mkdir(t, "alpha")
	reg := openTestRegistry(t)
	if _, err := reg.Register("alpha", dir, "", time.Now().UTC()); err != nil {
		t.Fatalf("register alpha: %v", err)
	}

	old := snapshot.ProjectSnapshot{
		ProjectID:       "alpha",
		UpdatedAt:       time.Now().UTC().Add(-time.Hour),
		RepositoryCount: 42,
	}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".punakawan"), 0o755); err != nil {
		t.Fatalf("mkdir .punakawan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".punakawan", snapshotFileName), data, 0o644); err != nil {
		t.Fatalf("write persisted snapshot: %v", err)
	}

	// Hold the deep Get at the door so the background revalidation cannot
	// race the assertion below that List served without a synchronous
	// recompute.
	gate := make(chan struct{})
	inner := &fakeWorkspaceReader{
		getCalls: map[string]int{},
		detail:   map[string]contract.WorkspaceDetail{"alpha": summaryFixture("alpha", 99)},
		gate:     gate,
	}
	c := NewCachedWorkspaceReader(inner, reg, "alpha", time.Millisecond)
	ctx := context.Background()

	out, err := c.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 1 || out[0].RepositoryCount != 42 {
		t.Fatalf("List = %+v, want RepositoryCount=42 served instantly from the persisted snapshot", out)
	}
	if got := inner.calls("alpha"); got != 0 {
		t.Fatalf("alpha Get calls right after List = %d, want 0 (persisted snapshot served without a live recompute)", got)
	}

	// Release the background revalidation and confirm it eventually replaces
	// the stale value with the freshly computed one.
	close(gate)
	waitForHealth(t, func() bool {
		out, err := c.List(ctx)
		return err == nil && len(out) == 1 && out[0].RepositoryCount == 99
	})

	// Drain any still-in-flight background refresh so its snapshot write
	// completes before t.TempDir cleanup removes the directory it targets.
	if _, err := c.cache.Refresh(ctx, "alpha"); err != nil {
		t.Fatalf("drain refresh: %v", err)
	}
}
