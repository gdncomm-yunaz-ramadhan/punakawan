package search

import "sort"

// rrfK is Reciprocal Rank Fusion's smoothing constant. 60 is the value
// used in the original RRF paper (Cormack, Clarke & Buettcher 2009) and
// most production uses since; it is not tuned per corpus here.
const rrfK = 60.0

// defaultFuseLimit bounds FuseRankedLists' output when no explicit limit is
// given, so a fused global-search response cannot grow without bound as more
// workspaces/corpora are added (punokawan-ssu).
const defaultFuseLimit = 50

// RankedList is one corpus's own ranked results for the same query -
// typically one per workspace when fusing across separate BM25F indexes,
// per punakawan-panel-implementation-plan.md §10.1: "BM25 scores from
// separate corpora are not directly comparable," so global search must
// combine rank position, not raw Score, across workspaces.
type RankedList struct {
	WorkspaceID string
	Results     []Result
}

// FusedResult is one RankedList entry after fusion, tagged with the
// workspace it came from and its RRF score.
type FusedResult struct {
	WorkspaceID string
	Result      Result
	RRFScore    float64
}

// FuseRankedLists combines lists into one globally ranked slice using
// Reciprocal Rank Fusion: each result's score is 1/(k+rank) within its own
// list. Unlike RRF's classic use (merging several rankings of the *same*
// item set), each workspace's results here are disjoint - every item
// belongs to exactly one input list - so this reduces to normalizing each
// workspace's own rank position onto a comparable 0-1 scale before a
// single global sort, which is exactly what §10.1 asks for.
//
// An optional limit caps how many top-ranked results are returned; when
// omitted or non-positive it defaults to defaultFuseLimit, so callers (like
// the panel's global search) get a bounded response without having to pass a
// limit themselves (punokawan-ssu). The variadic form keeps existing call
// sites that pass only lists compiling unchanged.
func FuseRankedLists(lists []RankedList, limit ...int) []FusedResult {
	max := defaultFuseLimit
	if len(limit) > 0 && limit[0] > 0 {
		max = limit[0]
	}

	fused := make([]FusedResult, 0)
	for _, list := range lists {
		for i, r := range list.Results {
			rank := i + 1
			fused = append(fused, FusedResult{
				WorkspaceID: list.WorkspaceID,
				Result:      r,
				RRFScore:    1.0 / (rrfK + float64(rank)),
			})
		}
	}
	sort.SliceStable(fused, func(i, j int) bool { return fused[i].RRFScore > fused[j].RRFScore })
	if len(fused) > max {
		fused = fused[:max]
	}
	return fused
}
