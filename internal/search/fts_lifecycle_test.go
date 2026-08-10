package search

import "testing"

// TestSearchUnicodeQueryMatchesRecord guards against a regression specific to
// the Bleve->SQLite FTS5 migration: FTS5's default unicode61 tokenizer must
// still recover matches over non-ASCII text stored by IndexRecord, since
// tokenization now happens in Go (Tokenize) before FTS5 ever sees the text
// (see ftsValues/tokenizedStream in index.go). Tokenize only splits on
// whitespace and a fixed set of ASCII delimiters (: / . - _), so this uses
// whitespace-delimited accented words rather than a script with no word
// boundaries of its own (e.g. CJK) - that's Tokenize's existing behavior,
// unchanged by this migration and out of scope here (tracked separately).
func TestSearchUnicodeQueryMatchesRecord(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := newRecord(t, "REQ-UNICODE")
	rec.Title = "Café über naïve résumé approval"
	rec.Content = strp("Le client demande un remboursement après réception de la commande.")
	putAndIndex(t, store, ix, rec)

	other := newRecord(t, "REQ-OTHER")
	other.Title = "Unrelated shipping label task"
	putAndIndex(t, store, ix, other)

	results, err := Search(store, ix, Request{Query: "über naïve résumé"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s ranked first for a Unicode query", results, rec.Id)
	}
}

// TestSearchTagFilterExcludesOtherTags covers the Tags side of buildFilters'
// hard-filter pair (TestSearchTypeFilterExcludesOtherTypes already covers
// Types) - both are implemented as an EXISTS(json_each(...)) predicate ANDed
// into the SQL before the fetch cap, so an untagged/mistagged record must
// never enter the candidate set at all.
func TestSearchTagFilterExcludesOtherTags(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	billing := newRecord(t, "REQ-5")
	billing.Title = "Invoice rounding rule"
	billing.Tags = []string{"billing"}
	putAndIndex(t, store, ix, billing)

	shipping := newRecord(t, "REQ-6")
	shipping.Title = "Invoice rounding rule for shipping labels"
	shipping.Tags = []string{"shipping"}
	putAndIndex(t, store, ix, shipping)

	results, err := Search(store, ix, Request{Query: "invoice rounding rule", Tags: []string{"billing"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Id == shipping.Id {
			t.Fatalf("results = %+v, want the shipping-tagged record excluded by the tag filter", results)
		}
	}
	if len(results) == 0 || results[0].Id != billing.Id {
		t.Fatalf("results = %+v, want %s to match", results, billing.Id)
	}
}

// TestIndexRecordUpdateAndDeleteReflectedInSearch exercises the full
// create->update->delete lifecycle against the external-content FTS5 table:
// upsertTx/deleteFTSIfPresent must keep knowledge_fts's tokenized values in
// exact sync with knowledge_search's raw fields on every mutation, not just
// on first insert.
func TestIndexRecordUpdateAndDeleteReflectedInSearch(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := newRecord(t, "REQ-LIFECYCLE")
	rec.Title = "Zylofenix onboarding checklist"
	putAndIndex(t, store, ix, rec)

	results, err := Search(store, ix, Request{Query: "zylofenix"})
	if err != nil {
		t.Fatalf("Search (create): %v", err)
	}
	if len(results) == 0 || results[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s to match after create", results, rec.Id)
	}

	rec.Title = "Quixoblat renewal checklist"
	putAndIndex(t, store, ix, rec)

	results, err = Search(store, ix, Request{Query: "quixoblat"})
	if err != nil {
		t.Fatalf("Search (update): %v", err)
	}
	if len(results) == 0 || results[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s to match the updated title", results, rec.Id)
	}

	// "zylofenix" is unique to the pre-update title; BM25's OR-across-tokens
	// semantics means a query sharing other tokens (e.g. "checklist") would
	// still match post-update and wouldn't prove the stale term was purged.
	results, err = Search(store, ix, Request{Query: "zylofenix"})
	if err != nil {
		t.Fatalf("Search (stale title): %v", err)
	}
	for _, r := range results {
		if r.Id == rec.Id {
			t.Fatalf("results = %+v, want the old title to no longer match after update", results)
		}
	}

	if err := ix.DeleteRecord(rec.Id); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	results, err = Search(store, ix, Request{Query: "quixoblat"})
	if err != nil {
		t.Fatalf("Search (delete): %v", err)
	}
	for _, r := range results {
		if r.Id == rec.Id {
			t.Fatalf("results = %+v, want %s gone after DeleteRecord", results, rec.Id)
		}
	}
}
