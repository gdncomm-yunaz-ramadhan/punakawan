package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func putAgedFixtureRecord(t *testing.T, store *knowledge.Store, id, title string, state protocol.KnowledgeRecordValidityState, ageDays int, relations []protocol.KnowledgeRecordRelationsElem) protocol.KnowledgeRecord {
	t.Helper()
	rec := protocol.KnowledgeRecord{
		Id:        "pkw:req/fixture/" + id,
		Type:      protocol.KnowledgeRecordTypeRequirement,
		Status:    "active",
		Title:     title,
		Relations: relations,
		Source:    protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC().AddDate(0, 0, -ageDays)},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{State: state},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put %s: %v", id, err)
	}
	return rec
}

func TestFindPruneCandidatesSurfacesValidityAndAgeSignals(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}

	stale := putAgedFixtureRecord(t, store, "STALE-1", "Stale finding", protocol.KnowledgeRecordValidityStateStale, 200, nil)
	fresh := putAgedFixtureRecord(t, store, "FRESH-1", "Fresh finding", protocol.KnowledgeRecordValidityStateObserved, 1, nil)

	h := findPruneCandidatesHandler(a)
	_, out, err := h(context.Background(), nil, FindPruneCandidatesInput{})
	if err != nil {
		t.Fatalf("find_prune_candidates: %v", err)
	}

	byID := map[string]PruneCandidate{}
	for _, c := range out.Candidates {
		byID[c.Id] = c
	}
	if len(byID) != 2 {
		t.Fatalf("expected both records surfaced regardless of validity_state, got %d: %+v", len(byID), out.Candidates)
	}

	staleCand, ok := byID[stale.Id]
	if !ok {
		t.Fatalf("stale record missing from candidates: %+v", out.Candidates)
	}
	if staleCand.AgeDays < 199 || staleCand.AgeDays > 201 {
		t.Fatalf("expected age ~200 days, got %d", staleCand.AgeDays)
	}
	if !containsSignal(staleCand.Signals, "validity_state:stale") {
		t.Fatalf("expected a validity_state signal, got %+v", staleCand.Signals)
	}
	if !containsAgeSignal(staleCand.Signals) {
		t.Fatalf("expected an age signal past the heuristic threshold, got %+v", staleCand.Signals)
	}
	if !containsSignal(staleCand.Signals, "no incoming references") {
		t.Fatalf("expected 'no incoming references' since nothing points at it, got %+v", staleCand.Signals)
	}

	freshCand, ok := byID[fresh.Id]
	if !ok {
		t.Fatalf("fresh record missing from candidates: %+v", out.Candidates)
	}
	if len(freshCand.Signals) != 1 || freshCand.Signals[0] != "no incoming references" {
		t.Fatalf("fresh, observed, unreferenced record should carry only the no-incoming-references signal, got %+v", freshCand.Signals)
	}
}

func TestFindPruneCandidatesFiltersByValidityState(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	stale := putAgedFixtureRecord(t, store, "STALE-2", "Stale", protocol.KnowledgeRecordValidityStateStale, 5, nil)
	putAgedFixtureRecord(t, store, "FRESH-2", "Fresh", protocol.KnowledgeRecordValidityStateObserved, 5, nil)

	h := findPruneCandidatesHandler(a)
	_, out, err := h(context.Background(), nil, FindPruneCandidatesInput{ValidityState: "stale"})
	if err != nil {
		t.Fatalf("find_prune_candidates: %v", err)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].Id != stale.Id {
		t.Fatalf("expected only the stale record, got %+v", out.Candidates)
	}
}

func TestFindPruneCandidatesReportsRelationCount(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}

	target := putAgedFixtureRecord(t, store, "TARGET-1", "Referenced record", protocol.KnowledgeRecordValidityStateObserved, 1, nil)
	putAgedFixtureRecord(t, store, "REFERRER-1", "Referencing record", protocol.KnowledgeRecordValidityStateObserved, 1, []protocol.KnowledgeRecordRelationsElem{
		{Target: target.Id, Type: protocol.KnowledgeRecordRelationsElemTypeDependsOn},
	})

	h := findPruneCandidatesHandler(a)
	_, out, err := h(context.Background(), nil, FindPruneCandidatesInput{})
	if err != nil {
		t.Fatalf("find_prune_candidates: %v", err)
	}

	for _, c := range out.Candidates {
		if c.Id == target.Id {
			if c.RelationCount != 1 {
				t.Fatalf("expected relation_count=1 for the referenced record, got %d", c.RelationCount)
			}
			if containsSignal(c.Signals, "no incoming references") {
				t.Fatalf("a referenced record must not carry the no-incoming-references signal, got %+v", c.Signals)
			}
		}
	}
}

func TestFindPruneCandidatesMinAgeDaysFiltersWithinPage(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	old := putAgedFixtureRecord(t, store, "OLD-1", "Old", protocol.KnowledgeRecordValidityStateObserved, 300, nil)
	putAgedFixtureRecord(t, store, "NEW-1", "New", protocol.KnowledgeRecordValidityStateObserved, 1, nil)

	h := findPruneCandidatesHandler(a)
	_, out, err := h(context.Background(), nil, FindPruneCandidatesInput{MinAgeDays: 100})
	if err != nil {
		t.Fatalf("find_prune_candidates: %v", err)
	}
	if out.Scanned != 2 {
		t.Fatalf("expected scanned to reflect the fetched page (2) before age filtering, got %d", out.Scanned)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].Id != old.Id {
		t.Fatalf("expected only the old record past min_age_days, got %+v", out.Candidates)
	}
}

func TestFindPruneCandidatesOutputFeedsDeleteKnowledge(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := putAgedFixtureRecord(t, store, "DELETEME-1", "To delete", protocol.KnowledgeRecordValidityStateSuperseded, 5, nil)

	findHandler := findPruneCandidatesHandler(a)
	_, found, err := findHandler(context.Background(), nil, FindPruneCandidatesInput{ValidityState: "superseded"})
	if err != nil {
		t.Fatalf("find_prune_candidates: %v", err)
	}
	if len(found.Candidates) != 1 || found.Candidates[0].Id != rec.Id {
		t.Fatalf("expected exactly the superseded record, got %+v", found.Candidates)
	}

	deleteHandler := deleteKnowledgeHandler(a)
	_, delOut, err := deleteHandler(context.Background(), nil, DeleteKnowledgeInput{Ids: []string{found.Candidates[0].Id}})
	if err != nil {
		t.Fatalf("delete_knowledge: %v", err)
	}
	if len(delOut.Deleted) != 1 || delOut.Deleted[0] != rec.Id {
		t.Fatalf("expected the candidate id to delete cleanly, got %+v", delOut)
	}
	if _, err := store.Get(rec.Id); err == nil {
		t.Fatal("expected the record to be gone after deleting the prune candidate")
	}
}

func containsSignal(signals []string, want string) bool {
	for _, s := range signals {
		if s == want {
			return true
		}
	}
	return false
}

func containsAgeSignal(signals []string) bool {
	for _, s := range signals {
		if len(s) >= 9 && s[:9] == "retrieved" {
			return true
		}
	}
	return false
}
