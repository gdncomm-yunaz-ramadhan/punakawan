package search

import (
	"testing"
)

// --- pure unit tests (no dolt required) ---

func TestFetchLimitScalesWithLimitFlooredAtMin(t *testing.T) {
	if got := fetchLimit(0); got != minFetchCap {
		t.Fatalf("fetchLimit(0) = %d, want the default-limit floor %d", got, minFetchCap)
	}
	if got := fetchLimit(10); got != minFetchCap {
		t.Fatalf("fetchLimit(10) = %d, want floored at %d", got, minFetchCap)
	}
	if got := fetchLimit(100); got != 100*fetchCapMultiplier {
		t.Fatalf("fetchLimit(100) = %d, want %d (a multiple of the limit)", got, 100*fetchCapMultiplier)
	}
}

func TestMergeHitsKeepsExistingHit(t *testing.T) {
	dst := map[string]hitInfo{"a": {score: 10}}
	src := map[string]hitInfo{"a": {score: 1}, "b": {score: 2}}
	mergeHits(dst, src)

	if dst["a"].score != 10 {
		t.Fatalf("dst[a].score = %v, want the existing BM25 hit (10) preserved, not overwritten", dst["a"].score)
	}
	if _, ok := dst["b"]; !ok {
		t.Fatalf("dst missing b: identifier-only hits should be unioned in")
	}
}

func TestBuildIdentifierQueryNilWithoutIdentifiers(t *testing.T) {
	if q := buildIdentifierQuery(nil); q != nil {
		t.Fatalf("buildIdentifierQuery(nil) = %v, want nil", q)
	}
	if q := buildIdentifierQuery([]Identifier{{Kind: IdentifierKindCVE, Value: "CVE-2026-0001"}}); q == nil {
		t.Fatalf("buildIdentifierQuery(identifiers) = nil, want a query")
	}
}

func TestStoredDocFieldParsing(t *testing.T) {
	fields := map[string]interface{}{
		"title":   "one",
		"aliases": "solo",
		"symbols": []interface{}{"A", "B"},
		"num":     3,
	}
	if got := stringField(fields, "title"); got != "one" {
		t.Fatalf("stringField(title) = %q, want %q", got, "one")
	}
	if got := stringField(fields, "num"); got != "" {
		t.Fatalf("stringField(num) = %q, want empty for a non-string value", got)
	}
	if got := stringSliceField(fields, "aliases"); len(got) != 1 || got[0] != "solo" {
		t.Fatalf("stringSliceField(aliases) = %v, want [solo]", got)
	}
	if got := stringSliceField(fields, "symbols"); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Fatalf("stringSliceField(symbols) = %v, want [A B]", got)
	}
	if got := stringSliceField(fields, "missing"); got != nil {
		t.Fatalf("stringSliceField(missing) = %v, want nil", got)
	}
}

// --- dolt-backed integration tests ---

// TestRebuildSkipsWhenStoreUnchanged proves the watermark gate
// (punokawan-77q): once synced, a Rebuild with an unchanged store is a no-op,
// while a real mutation forces a resync.
func TestRebuildSkipsWhenStoreUnchanged(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := newRecord(t, "WM-1")
	rec.Title = "Watermark canary record"
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := Rebuild(store, ix); err != nil {
		t.Fatalf("first Rebuild: %v", err)
	}

	// Drift the index out-of-band. A watermark-gated Rebuild against an
	// unchanged store must NOT repair this - it should short-circuit.
	if err := ix.bleve.Delete(rec.Id); err != nil {
		t.Fatalf("bleve.Delete: %v", err)
	}
	if err := Rebuild(store, ix); err != nil {
		t.Fatalf("no-op Rebuild: %v", err)
	}
	drifted, err := Search(store, ix, Request{Query: "watermark canary"})
	if err != nil {
		t.Fatalf("Search after no-op Rebuild: %v", err)
	}
	if len(drifted) != 0 {
		t.Fatalf("results = %+v, want none: the no-op Rebuild should have skipped re-indexing", drifted)
	}

	// Mutate the store: count and newest updated_at change, so the next
	// Rebuild must resync and restore WM-1 (plus index WM-2).
	rec2 := newRecord(t, "WM-2")
	rec2.Title = "Second canary forces resync"
	if err := store.Put(rec2); err != nil {
		t.Fatalf("Put second: %v", err)
	}
	if err := Rebuild(store, ix); err != nil {
		t.Fatalf("resync Rebuild: %v", err)
	}
	restored, err := Search(store, ix, Request{Query: "watermark canary"})
	if err != nil {
		t.Fatalf("Search after resync: %v", err)
	}
	if len(restored) == 0 || restored[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s restored after a store change forced a resync", restored, rec.Id)
	}
}

// TestSearchHydratesFullRecord proves finalize hydrates Result.Record from the
// store (punokawan-co7): a field that scoring never reads and Bleve does not
// return in storedFields (Source.Provider) must still be present, which is
// only possible if the full record was fetched.
func TestSearchHydratesFullRecord(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := newRecord(t, "HY-1")
	rec.Title = "Hydrate the whole record"
	rec.Source.Provider = "sonarqube"
	putAndIndex(t, store, ix, rec)

	results, err := Search(store, ix, Request{Query: "hydrate whole record"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s", results, rec.Id)
	}
	if results[0].Record.Id != rec.Id {
		t.Fatalf("Record.Id = %q, want the full record hydrated (%q)", results[0].Record.Id, rec.Id)
	}
	if results[0].Record.Source.Provider != "sonarqube" {
		t.Fatalf("Record.Source.Provider = %q, want %q from the hydrated store record", results[0].Record.Source.Provider, "sonarqube")
	}
}
