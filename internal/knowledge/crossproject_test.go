package knowledge

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func newHubRecord(id, title string) protocol.KnowledgeRecord {
	return protocol.KnowledgeRecord{
		Id: id, Type: protocol.KnowledgeRecordTypeRequirement, Status: "active", Title: title,
		Source:     protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
}

func TestOwnProjectReportsTheBoundDatabaseForBothLegacyAndHub(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	sup := tools.New(root)
	legacy, err := Open(sup, filepath.Join(root, ".punakawan", "knowledge"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	if got := legacy.OwnProject(); got != "knowledge" {
		t.Fatalf("legacy OwnProject: got %q, want %q", got, "knowledge")
	}

	hubDir := filepath.Join(root, "hub")
	hubStore, err := OpenInHub(sup, hubDir, "proj-x")
	if err != nil {
		t.Fatalf("OpenInHub: %v", err)
	}
	t.Cleanup(func() { _ = hubStore.Close() })
	if got := hubStore.OwnProject(); got != "proj-x" {
		t.Fatalf("hub OwnProject: got %q, want %q", got, "proj-x")
	}
}

func TestGetInProjectDefaultsToOwnProjectWhenEmpty(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	sup := tools.New(root)
	hubDir := filepath.Join(root, "hub")
	store, err := OpenInHub(sup, hubDir, "proj-a")
	if err != nil {
		t.Fatalf("OpenInHub: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	rec := newHubRecord("pkw:req/fixture/OWN-1", "own record")
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

func TestGetInProjectReachesASiblingHubProject(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	sup := tools.New(root)
	hubDir := filepath.Join(root, "hub")

	storeA, err := OpenInHub(sup, hubDir, "proj-a")
	if err != nil {
		t.Fatalf("OpenInHub proj-a: %v", err)
	}
	t.Cleanup(func() { _ = storeA.Close() })
	storeB, err := OpenInHub(sup, hubDir, "proj-b")
	if err != nil {
		t.Fatalf("OpenInHub proj-b: %v", err)
	}
	t.Cleanup(func() { _ = storeB.Close() })

	rec := newHubRecord("pkw:req/fixture/CROSS-1", "cross project record")
	if err := storeB.Put(rec); err != nil {
		t.Fatalf("Put on proj-b: %v", err)
	}

	// storeA has never seen this record in its own database, but can reach
	// it by deliberately naming proj-b - the whole point of ADR-0020's
	// project filter.
	got, err := storeA.GetInProject("proj-b", rec.Id)
	if err != nil {
		t.Fatalf("GetInProject(proj-b) from storeA: %v", err)
	}
	if got.Id != rec.Id || got.Title != rec.Title {
		t.Fatalf("got %+v, want id=%s title=%s", got, rec.Id, rec.Title)
	}

	// And storeA's own database still does not have it under an unqualified Get.
	if _, err := storeA.Get(rec.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected proj-a's own Get to NOT see proj-b's record, got err=%v", err)
	}
}

func TestGetInProjectRejectsUnsafeProjectName(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	sup := tools.New(root)
	store, err := OpenInHub(sup, filepath.Join(root, "hub"), "proj-a")
	if err != nil {
		t.Fatalf("OpenInHub: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.GetInProject("proj`; drop", "some-id"); err == nil {
		t.Fatal("expected an error for an unsafe project name")
	}
}

func TestSearchInProjectFindsSubstringMatchesInASiblingProject(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	sup := tools.New(root)
	hubDir := filepath.Join(root, "hub")

	storeA, err := OpenInHub(sup, hubDir, "proj-a")
	if err != nil {
		t.Fatalf("OpenInHub proj-a: %v", err)
	}
	t.Cleanup(func() { _ = storeA.Close() })
	storeB, err := OpenInHub(sup, hubDir, "proj-b")
	if err != nil {
		t.Fatalf("OpenInHub proj-b: %v", err)
	}
	t.Cleanup(func() { _ = storeB.Close() })

	rec := newHubRecord("pkw:req/fixture/SEARCH-1", "unique-marker-zephyr")
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
	requireDoltForHubTest(t)

	root := t.TempDir()
	sup := tools.New(root)
	hubDir := filepath.Join(root, "hub")
	store, err := OpenInHub(sup, hubDir, "proj-a")
	if err != nil {
		t.Fatalf("OpenInHub: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	req := newHubRecord("pkw:req/fixture/TYPE-REQ", "shared-term-quokka")
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
