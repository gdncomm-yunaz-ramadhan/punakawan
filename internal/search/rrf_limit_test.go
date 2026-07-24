package search

import (
	"strconv"
	"testing"
)

func manyResults(n int) []RankedList {
	results := make([]Result, n)
	for i := range results {
		results[i] = Result{Id: strconv.Itoa(i)}
	}
	return []RankedList{{WorkspaceID: "ws", Results: results}}
}

func TestFuseRankedListsAppliesExplicitLimit(t *testing.T) {
	// punokawan-ssu: an explicit limit caps the fused result count.
	fused := FuseRankedLists(manyResults(10), 3)
	if len(fused) != 3 {
		t.Fatalf("len(fused) = %d, want 3 (explicit limit)", len(fused))
	}
}

func TestFuseRankedListsDefaultLimitCapsUnboundedInput(t *testing.T) {
	// With no limit given, the default (50) bounds the response even for a
	// large corpus, so it cannot grow without bound (punokawan-ssu).
	fused := FuseRankedLists(manyResults(120))
	if len(fused) != defaultFuseLimit {
		t.Fatalf("len(fused) = %d, want %d (default limit)", len(fused), defaultFuseLimit)
	}
}

func TestFuseRankedListsNonPositiveLimitFallsBackToDefault(t *testing.T) {
	fused := FuseRankedLists(manyResults(120), 0)
	if len(fused) != defaultFuseLimit {
		t.Fatalf("len(fused) = %d, want %d (non-positive limit falls back to default)", len(fused), defaultFuseLimit)
	}
}
