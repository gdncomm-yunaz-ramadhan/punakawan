package knowledge

import (
	"strings"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func strptr(s string) *string { return &s }

func TestBoundedSummaryPrefersSummaryThenContentThenTitle(t *testing.T) {
	// Summary wins.
	r := protocol.KnowledgeRecord{Title: "T", Summary: strptr("the summary"), Content: strptr("the content")}
	if got := BoundedSummary(r); got == nil || *got != "the summary" {
		t.Fatalf("want summary, got %v", got)
	}
	// Content when no summary.
	r = protocol.KnowledgeRecord{Title: "T", Content: strptr("the content")}
	if got := BoundedSummary(r); got == nil || *got != "the content" {
		t.Fatalf("want content, got %v", got)
	}
	// Title as last resort.
	r = protocol.KnowledgeRecord{Title: "just a title"}
	if got := BoundedSummary(r); got == nil || *got != "just a title" {
		t.Fatalf("want title, got %v", got)
	}
	// Nothing usable.
	if got := BoundedSummary(protocol.KnowledgeRecord{}); got != nil {
		t.Fatalf("want nil for empty record, got %v", got)
	}
}

func TestBoundedSummaryTypedPayload(t *testing.T) {
	// No summary/content: typed payload provides the line, beating Title.
	r := protocol.KnowledgeRecord{
		Title:      "petruk plan title",
		PetrukPlan: &protocol.KnowledgeRecordPetrukPlan{RecommendedSolution: strptr("adopt approach X")},
	}
	if got := BoundedSummary(r); got == nil || *got != "adopt approach X" {
		t.Fatalf("want typed payload summary, got %v", got)
	}
}

func TestBoundedSummaryClips(t *testing.T) {
	long := strings.Repeat("a", maxSummaryLen+50)
	r := protocol.KnowledgeRecord{Summary: strptr(long)}
	got := BoundedSummary(r)
	if got == nil {
		t.Fatal("nil summary")
	}
	if len([]rune(*got)) > maxSummaryLen+1 { // +1 for the ellipsis rune
		t.Fatalf("summary not clipped: %d runes", len([]rune(*got)))
	}
	if !strings.HasSuffix(*got, "…") {
		t.Fatalf("clipped summary should end with ellipsis")
	}
}
