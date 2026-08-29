package planimport

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
)

func newTestPlanStore(t *testing.T) *plan.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return plan.NewStore(db)
}

func writeLegacyVersion(t *testing.T, workspaceRoot, legacyPlanID string, version int, content string) {
	t.Helper()
	dir := filepath.Join(workspaceRoot, ".punakawan", "plans", legacyPlanID, "versions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, itoa(version)+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func itoa(n int) string {
	digits := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestImportCarriesEveryVersionInOrder(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeLegacyVersion(t, root, "plan-a", 1, "# plan a v1")
	writeLegacyVersion(t, root, "plan-a", 2, "# plan a v2")
	writeLegacyVersion(t, root, "plan-b", 1, "# plan b v1")

	plans := newTestPlanStore(t)
	records, err := Import(ctx, plans, "ws-1", root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %+v, want 3", records)
	}

	canonicalA := CanonicalPlanID("ws-1", "plan-a")
	v1, err := plans.GetRevision(ctx, canonicalA, 1)
	if err != nil {
		t.Fatalf("GetRevision(plan-a, 1): %v", err)
	}
	if v1.LegacyMarkdown != "# plan a v1" {
		t.Fatalf("plan-a revision 1 legacy_markdown = %q, want %q", v1.LegacyMarkdown, "# plan a v1")
	}
	v2, err := plans.GetRevision(ctx, canonicalA, 2)
	if err != nil {
		t.Fatalf("GetRevision(plan-a, 2): %v", err)
	}
	if v2.LegacyMarkdown != "# plan a v2" {
		t.Fatalf("plan-a revision 2 legacy_markdown = %q, want %q", v2.LegacyMarkdown, "# plan a v2")
	}

	head, err := plans.Get(ctx, canonicalA)
	if err != nil {
		t.Fatalf("Get(plan-a): %v", err)
	}
	if head.Revision != 2 {
		t.Fatalf("plan-a head revision = %d, want 2", head.Revision)
	}
}

func TestImportIsIdempotentOnRepeatedRun(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeLegacyVersion(t, root, "plan-a", 1, "# plan a v1")
	writeLegacyVersion(t, root, "plan-a", 2, "# plan a v2")

	plans := newTestPlanStore(t)
	first, err := Import(ctx, plans, "ws-1", root)
	if err != nil {
		t.Fatalf("first Import: %v", err)
	}
	second, err := Import(ctx, plans, "ws-1", root)
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if len(second) != len(first) {
		t.Fatalf("second Import returned %d records, want %d (same as first)", len(second), len(first))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("record %d differs across runs: %+v vs %+v", i, first[i], second[i])
		}
	}

	canonicalA := CanonicalPlanID("ws-1", "plan-a")
	head, err := plans.Get(ctx, canonicalA)
	if err != nil {
		t.Fatalf("Get(plan-a): %v", err)
	}
	if head.Revision != 2 {
		t.Fatalf("plan-a head revision after re-import = %d, want still 2 (no duplicate revisions)", head.Revision)
	}
}

func TestImportDisambiguatesCollidingLegacyPlanIDsAcrossWorkspaces(t *testing.T) {
	ctx := context.Background()
	rootA := t.TempDir()
	rootB := t.TempDir()
	writeLegacyVersion(t, rootA, "plan-a", 1, "# workspace one's plan a")
	writeLegacyVersion(t, rootB, "plan-a", 1, "# workspace two's plan a")

	plans := newTestPlanStore(t)
	if _, err := Import(ctx, plans, "ws-1", rootA); err != nil {
		t.Fatalf("Import ws-1: %v", err)
	}
	if _, err := Import(ctx, plans, "ws-2", rootB); err != nil {
		t.Fatalf("Import ws-2: %v", err)
	}

	one, err := plans.Get(ctx, CanonicalPlanID("ws-1", "plan-a"))
	if err != nil {
		t.Fatalf("Get ws-1 plan-a: %v", err)
	}
	two, err := plans.Get(ctx, CanonicalPlanID("ws-2", "plan-a"))
	if err != nil {
		t.Fatalf("Get ws-2 plan-a: %v", err)
	}
	if one.LegacyMarkdown == two.LegacyMarkdown {
		t.Fatalf("both workspaces' plan-a resolved to the same content: %q", one.LegacyMarkdown)
	}
	if one.LegacyMarkdown != "# workspace one's plan a" || two.LegacyMarkdown != "# workspace two's plan a" {
		t.Fatalf("content mismatch: one=%q two=%q", one.LegacyMarkdown, two.LegacyMarkdown)
	}
}

func TestImportEmptyWorkspaceReturnsNoRecords(t *testing.T) {
	ctx := context.Background()
	plans := newTestPlanStore(t)
	records, err := Import(ctx, plans, "ws-1", t.TempDir())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %+v, want empty", records)
	}
}
