package artifact

import "testing"

func TestDiffLinesIdenticalContentIsAllEqual(t *testing.T) {
	lines, summary := DiffLines("a\nb\nc", "a\nb\nc")
	for _, l := range lines {
		if l.Kind != DiffLineEqual {
			t.Fatalf("line %+v is not equal for identical content", l)
		}
	}
	if summary.Added != 0 || summary.Removed != 0 {
		t.Fatalf("summary = %+v, want all-zero for identical content", summary)
	}
}

func TestDiffLinesDetectsAdditionsAndRemovals(t *testing.T) {
	lines, summary := DiffLines("a\nb\nc", "a\nx\nc")
	if summary.Added != 1 || summary.Removed != 1 {
		t.Fatalf("summary = %+v, want 1 added and 1 removed for a single-line change", summary)
	}

	var kinds []DiffLineKind
	for _, l := range lines {
		kinds = append(kinds, l.Kind)
	}
	wantOrder := []DiffLineKind{DiffLineEqual, DiffLineAdded, DiffLineRemoved, DiffLineEqual}
	if len(kinds) != len(wantOrder) {
		t.Fatalf("kinds = %v, want %v", kinds, wantOrder)
	}
	for i := range kinds {
		if kinds[i] != wantOrder[i] {
			t.Fatalf("kinds = %v, want %v", kinds, wantOrder)
		}
	}
}

func TestDiffLinesPureAddition(t *testing.T) {
	_, summary := DiffLines("a", "a\nb\nc")
	if summary.Added != 2 || summary.Removed != 0 {
		t.Fatalf("summary = %+v, want 2 added, 0 removed", summary)
	}
}

func TestUnifiedDiffRendersPrefixedLines(t *testing.T) {
	lines, _ := DiffLines("a\nb", "a\nc")
	got := UnifiedDiff(lines)
	want := " a\n+c\n-b\n"
	if got != want {
		t.Fatalf("UnifiedDiff = %q, want %q", got, want)
	}
}
