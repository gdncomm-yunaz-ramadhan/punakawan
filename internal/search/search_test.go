package search

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestSearchMatchesByBM25OnTitle(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := newRecord(t, "REQ-1")
	rec.Title = "Refund an approved order"
	rec.Content = strp("When a customer requests a refund on an approved order, issue the payment back.")
	putAndIndex(t, store, ix, rec)

	other := newRecord(t, "REQ-2")
	other.Title = "Unrelated shipping label task"
	putAndIndex(t, store, ix, other)

	results, err := Search(store, ix, Request{Query: "refund approved order"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s ranked first", results, rec.Id)
	}
	if results[0].Match.Kind != MatchKindBM25 {
		t.Fatalf("Match.Kind = %q, want bm25", results[0].Match.Kind)
	}
}

func TestSearchExactIdentifierOutranksBM25(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	withCVE := newRecord(t, "SEC-1")
	withCVE.Title = "Security finding"
	withCVE.Content = strp("Fixes CVE-2026-12345 in the auth middleware.")
	putAndIndex(t, store, ix, withCVE)

	generic := newRecord(t, "SEC-2")
	generic.Title = "Security finding follow-up"
	generic.Content = strp("Discusses the auth middleware fix in general terms.")
	putAndIndex(t, store, ix, generic)

	results, err := Search(store, ix, Request{Query: "CVE-2026-12345"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Id != withCVE.Id {
		t.Fatalf("results = %+v, want %s ranked first for its exact CVE match", results, withCVE.Id)
	}
	if results[0].Match.Kind != MatchKindIdentifier {
		t.Fatalf("Match.Kind = %q, want identifier", results[0].Match.Kind)
	}
	if results[0].Score <= exactIdentifierBonus {
		t.Fatalf("Score = %v, want at least the exact-identifier bonus of %v on top of its BM25 score", results[0].Score, exactIdentifierBonus)
	}
}

func TestSearchAliasReceivesBonusAndKind(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := newRecord(t, "GLOSS-1")
	rec.Title = "Adaptive Setara Assistance"
	rec.Aliases = []string{"ASA"}
	putAndIndex(t, store, ix, rec)

	results, err := Search(store, ix, Request{Query: "ASA"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s to match via its alias", results, rec.Id)
	}
	if results[0].Match.Kind != MatchKindAlias {
		t.Fatalf("Match.Kind = %q, want alias", results[0].Match.Kind)
	}
}

func TestSearchScopeBoostRanksSameRepositoryFirst(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	inScope := newRecord(t, "MOD-1")
	inScope.Title = "Payment retry logic"
	inScope.Scope = &protocol.KnowledgeRecordScope{Repository: strp("checkout-service")}
	putAndIndex(t, store, ix, inScope)

	outOfScope := newRecord(t, "MOD-2")
	outOfScope.Title = "Payment retry logic mirror"
	outOfScope.Scope = &protocol.KnowledgeRecordScope{Repository: strp("unrelated-service")}
	putAndIndex(t, store, ix, outOfScope)

	results, err := Search(store, ix, Request{Query: "payment retry logic", Scope: Scope{Repository: "checkout-service"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("results = %+v, want both records to match", results)
	}
	if results[0].Id != inScope.Id {
		t.Fatalf("results[0] = %+v, want %s ranked first via the scope bonus", results[0], inScope.Id)
	}
}

func TestSearchFuzzyFallbackOnMisspelling(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := newRecord(t, "REQ-3")
	rec.Title = "Reconciliation job for ledger entries"
	putAndIndex(t, store, ix, rec)

	// "Reconsiliation" (misspelled) has no exact BM25 term match, so this
	// must fall through to §11.8's fuzzy fallback.
	results, err := Search(store, ix, Request{Query: "Reconsiliation"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s via fuzzy fallback", results, rec.Id)
	}
	if results[0].Match.Kind != MatchKindFuzzy {
		t.Fatalf("Match.Kind = %q, want fuzzy", results[0].Match.Kind)
	}
}

func TestSearchExpandsOneHopRelations(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	target := newRecord(t, "REQ-target")
	target.Title = "Zorbex quantum widget alignment"
	putAndIndex(t, store, ix, target)

	source := newRecord(t, "REQ-source")
	source.Title = "Payout scheduling window dependency"
	source.Relations = []protocol.KnowledgeRecordRelationsElem{
		{Type: protocol.KnowledgeRecordRelationsElemTypeDependsOn, Target: target.Id},
	}
	putAndIndex(t, store, ix, source)

	unrelated := newRecord(t, "REQ-unrelated")
	unrelated.Title = "Some other unrelated note"
	putAndIndex(t, store, ix, unrelated)

	withoutRelated, err := Search(store, ix, Request{Query: "payout scheduling window dependency", IncludeRelated: false})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range withoutRelated {
		if r.Id == target.Id {
			t.Fatalf("did not request relation expansion, but %s (unrelated by keywords) still appeared: %+v", target.Id, withoutRelated)
		}
	}

	withRelated, err := Search(store, ix, Request{Query: "payout scheduling window dependency", IncludeRelated: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var sawTargetAsRelated bool
	for _, r := range withRelated {
		if r.Id == target.Id && r.Match.Kind == MatchKindRelated {
			sawTargetAsRelated = true
		}
	}
	if !sawTargetAsRelated {
		t.Fatalf("results = %+v, want %s included via one-hop relation expansion", withRelated, target.Id)
	}
}

func TestSearchTypeFilterExcludesOtherTypes(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	requirement := newRecord(t, "REQ-4")
	requirement.Title = "Loyalty points expiry rule"
	requirement.Type = protocol.KnowledgeRecordTypeRequirement
	putAndIndex(t, store, ix, requirement)

	claim := newRecord(t, "CLAIM-1")
	claim.Title = "Loyalty points expiry rule assumption"
	claim.Type = protocol.KnowledgeRecordTypeClaim
	putAndIndex(t, store, ix, claim)

	results, err := Search(store, ix, Request{Query: "loyalty points expiry rule", Types: []string{string(protocol.KnowledgeRecordTypeRequirement)}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Id == claim.Id {
			t.Fatalf("results = %+v, want the claim excluded by the type filter", results)
		}
	}
	if len(results) == 0 || results[0].Id != requirement.Id {
		t.Fatalf("results = %+v, want %s to match", results, requirement.Id)
	}
}

func TestSearchEmptyQueryReturnsNoResults(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	results, err := Search(store, ix, Request{Query: "   "})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %+v, want none for an empty query", results)
	}
}

func TestRebuildIndexesEveryStoreRecord(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := newRecord(t, "REQ-5")
	rec.Title = "Warehouse capacity threshold"
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := Rebuild(store, ix); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	results, err := Search(store, ix, Request{Query: "warehouse capacity threshold"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s after Rebuild", results, rec.Id)
	}
}

func TestRebuildPrunesEntriesDeletedFromTheStore(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := newRecord(t, "REQ-6")
	rec.Title = "Ephemeral cache invalidation note"
	putAndIndex(t, store, ix, rec)

	preDelete, err := Search(store, ix, Request{Query: "ephemeral cache invalidation"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(preDelete) == 0 || preDelete[0].Id != rec.Id {
		t.Fatalf("results = %+v, want %s before deletion", preDelete, rec.Id)
	}

	if err := store.Delete(rec.Id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if err := Rebuild(store, ix); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	postDelete, err := Search(store, ix, Request{Query: "ephemeral cache invalidation"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range postDelete {
		if r.Id == rec.Id {
			t.Fatalf("results = %+v, want %s pruned from the index after Store.Delete + Rebuild", postDelete, rec.Id)
		}
	}
}
