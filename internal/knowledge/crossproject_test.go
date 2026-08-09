package knowledge

import (
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func crossRecord(id, title string) protocol.KnowledgeRecord {
	return protocol.KnowledgeRecord{
		Id: id, Type: protocol.KnowledgeRecordTypeRequirement, Status: "active", Title: title,
		Source:     protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
}

func TestGetInProjectDefaultsToOwnProjectWhenEmpty(t *testing.T) {
	store := New(newTestDB(t), "proj-a")

	rec := crossRecord("pkw:req/fixture/OWN-1", "own record")
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.GetInProject("", rec.Id)
	if err != nil {
		t.Fatalf("GetInProject with empty project: %v", err)
	}
	if got.Id != rec.Id {
		t.Fatalf("got %q, want %q", got.Id, rec.Id)
	}

	got2, err := store.GetInProject("proj-a", rec.Id)
	if err != nil {
		t.Fatalf("GetInProject with own project: %v", err)
	}
	if got2.Id != rec.Id {
		t.Fatalf("got %q, want %q", got2.Id, rec.Id)
	}
}

func TestGetInProjectReachesASiblingProject(t *testing.T) {
	db := newTestDB(t)
	storeA := New(db, "proj-a")
	storeB := New(db, "proj-b")

	rec := crossRecord("pkw:req/fixture/CROSS-1", "cross project record")
	if err := storeB.Put(rec); err != nil {
		t.Fatalf("Put on proj-b: %v", err)
	}

	// storeA has never seen this record in its own project, but can reach it by
	// deliberately naming proj-b.
	got, err := storeA.GetInProject("proj-b", rec.Id)
	if err != nil {
		t.Fatalf("GetInProject(proj-b) from storeA: %v", err)
	}
	if got.Id != rec.Id || got.Title != rec.Title {
		t.Fatalf("got %+v, want id=%s title=%s", got, rec.Id, rec.Title)
	}

	// And storeA's own project still does not have it under an unqualified Get.
	if _, err := storeA.Get(rec.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected proj-a's own Get to NOT see proj-b's record, got err=%v", err)
	}
}

func TestSearchInProjectFindsSubstringMatchesInASiblingProject(t *testing.T) {
	db := newTestDB(t)
	storeA := New(db, "proj-a")
	storeB := New(db, "proj-b")

	rec := crossRecord("pkw:req/fixture/SEARCH-1", "unique-marker-zephyr")
	if err := storeB.Put(rec); err != nil {
		t.Fatalf("Put on proj-b: %v", err)
	}

	results, err := storeA.SearchInProject("proj-b", "unique-marker-zephyr", nil, 0)
	if err != nil {
		t.Fatalf("SearchInProject: %v", err)
	}
	if len(results) != 1 || results[0].Id != rec.Id {
		t.Fatalf("got %+v, want exactly [%s]", results, rec.Id)
	}
}

func TestSearchInProjectFiltersByType(t *testing.T) {
	store := New(newTestDB(t), "proj-a")

	req := crossRecord("pkw:req/fixture/TYPE-REQ", "shared-term-quokka")
	req.Type = protocol.KnowledgeRecordTypeRequirement
	if err := store.Put(req); err != nil {
		t.Fatalf("Put requirement: %v", err)
	}

	results, err := store.SearchInProject("proj-a", "shared-term-quokka", []string{"decision"}, 0)
	if err != nil {
		t.Fatalf("SearchInProject: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results filtered to type=decision, got %+v", results)
	}

	results, err = store.SearchInProject("proj-a", "shared-term-quokka", []string{"requirement"}, 0)
	if err != nil {
		t.Fatalf("SearchInProject: %v", err)
	}
	if len(results) != 1 || results[0].Id != req.Id {
		t.Fatalf("got %+v, want exactly [%s]", results, req.Id)
	}
}
