package search

import "testing"

func TestFuseRankedListsOrdersByRankNotRawScore(t *testing.T) {
	// ws-a's top hit has a huge raw score (a big single-workspace corpus);
	// ws-b's top hit has a tiny raw score (a small corpus). RRF must still
	// rank both top-of-list results ahead of either list's second result,
	// since it fuses on rank position, not the incomparable raw scores.
	lists := []RankedList{
		{WorkspaceID: "ws-a", Results: []Result{
			{Id: "a-1", Score: 500},
			{Id: "a-2", Score: 480},
		}},
		{WorkspaceID: "ws-b", Results: []Result{
			{Id: "b-1", Score: 0.9},
			{Id: "b-2", Score: 0.1},
		}},
	}

	fused := FuseRankedLists(lists)
	if len(fused) != 4 {
		t.Fatalf("FuseRankedLists = %+v, want 4 results", fused)
	}

	rankOf := func(id string) int {
		for i, f := range fused {
			if f.Result.Id == id {
				return i
			}
		}
		t.Fatalf("id %q not found in %+v", id, fused)
		return -1
	}

	if rankOf("a-1") > rankOf("a-2") {
		t.Fatalf("a-1 should outrank a-2 within its own list: %+v", fused)
	}
	if rankOf("b-1") > rankOf("b-2") {
		t.Fatalf("b-1 should outrank b-2 within its own list: %+v", fused)
	}
	// Both list-leaders (rank 1 in their own corpus) should beat both
	// list-runners-up (rank 2), regardless of raw score magnitude.
	if rankOf("a-2") < rankOf("b-1") && rankOf("a-1") > rankOf("b-1") {
		t.Fatalf("a-2 (rank 2) should not outrank b-1 (rank 1) just because a-1 had a bigger raw score: %+v", fused)
	}
}

func TestFuseRankedListsEmptyInput(t *testing.T) {
	fused := FuseRankedLists(nil)
	if len(fused) != 0 {
		t.Fatalf("FuseRankedLists(nil) = %+v, want empty", fused)
	}
}

func TestFuseRankedListsSkipsEmptyLists(t *testing.T) {
	fused := FuseRankedLists([]RankedList{
		{WorkspaceID: "ws-empty", Results: nil},
		{WorkspaceID: "ws-a", Results: []Result{{Id: "a-1"}}},
	})
	if len(fused) != 1 || fused[0].Result.Id != "a-1" {
		t.Fatalf("FuseRankedLists = %+v, want only a-1", fused)
	}
}
