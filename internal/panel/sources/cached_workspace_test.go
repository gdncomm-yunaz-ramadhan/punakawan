package sources

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/panel/contract"
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
}

func (f *fakeWorkspaceReader) List(ctx context.Context) ([]contract.WorkspaceSummary, error) {
	out := make([]contract.WorkspaceSummary, 0, len(f.detail))
	for _, d := range f.detail {
		out = append(out, d.WorkspaceSummary)
	}
	return out, nil
}

func (f *fakeWorkspaceReader) Get(ctx context.Context, id string) (contract.WorkspaceDetail, error) {
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

func summaryFixture(id string, knowledge, blocked int) contract.WorkspaceDetail {
	return contract.WorkspaceDetail{WorkspaceSummary: contract.WorkspaceSummary{
		ID:               id,
		Availability:     protocol.PanelSourceHealthAvailabilityAvailable,
		KnowledgeCount:   knowledge,
		BlockedTaskCount: blocked,
		LastActivityAt:   time.Now().UTC(),
	}}
}

func TestCachedWorkspaceReaderServesFromCache(t *testing.T) {
	inner := &fakeWorkspaceReader{
		getCalls: map[string]int{},
		detail: map[string]contract.WorkspaceDetail{
			"alpha": summaryFixture("alpha", 42, 1),
			"beta":  summaryFixture("beta", 7, 0),
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
	if byID["alpha"].KnowledgeCount != 42 || byID["alpha"].BlockedTaskCount != 1 {
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
		detail:   map[string]contract.WorkspaceDetail{"alpha": summaryFixture("alpha", 5, 0)},
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
