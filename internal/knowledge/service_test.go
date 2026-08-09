package knowledge

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newTestStore opens the shared SQLite storage kernel in a temp dir and scopes
// a Store to a fixed test project id - the same pattern taskstore's tests use,
// with no external dolt binary and no t.Skip.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return newTestStoreForProject(t, newTestDB(t), "test-project")
}

// newTestDB opens a shared kernel over a fresh temp database file.
func newTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestStoreForProject(t *testing.T, db *storage.DB, projectID string) *Store {
	t.Helper()
	return New(db, projectID)
}

func TestKnowledgeStoreCRUD(t *testing.T) {
	store := newTestStore(t)

	rec := protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/REQ-1",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Refund an approved order",
		Source: protocol.KnowledgeRecordSource{
			Provider:    "jira",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State:      protocol.KnowledgeRecordValidityStateVerified,
			VerifiedBy: []string{"gareng"},
		},
	}

	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != rec.Title || got.Type != rec.Type {
		t.Fatalf("unexpected record: %+v", got)
	}

	// Put again with a changed field to exercise the upsert path.
	rec.Status = "superseded"
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put (update): %v", err)
	}
	got, err = store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Status != "superseded" {
		t.Fatalf("expected updated status, got %q", got.Status)
	}

	list, err := store.ListByType(protocol.KnowledgeRecordTypeRequirement)
	if err != nil {
		t.Fatalf("ListByType: %v", err)
	}
	if len(list) != 1 || list[0].Id != rec.Id {
		t.Fatalf("unexpected list result: %+v", list)
	}

	if err := store.Delete(rec.Id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(rec.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestKnowledgeGetNotFound(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.Get("pkw:req/fixture/does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestKnowledgePutRejectsInvalidRecord(t *testing.T) {
	store := newTestStore(t)

	rec := validRecord()
	rec.Title = ""
	if err := store.Put(rec); err == nil {
		t.Fatal("expected Put to reject a record missing required provenance fields")
	}

	rec = validRecord()
	rec.Validity.State = protocol.KnowledgeRecordValidityStateVerified
	rec.Validity.VerifiedBy = nil
	if err := store.Put(rec); err == nil {
		t.Fatal("expected Put to reject a verified record with no verified_by")
	}
}

func TestKnowledgeRelationsRoundTrip(t *testing.T) {
	store := newTestStore(t)

	target := validRecord()
	target.Id = "pkw:req/fixture/REQ-target"
	if err := store.Put(target); err != nil {
		t.Fatalf("Put target: %v", err)
	}

	source := validRecord()
	source.Id = "pkw:req/fixture/REQ-source"
	source.Relations = []protocol.KnowledgeRecordRelationsElem{
		{Type: protocol.KnowledgeRecordRelationsElemTypeDependsOn, Target: target.Id},
	}
	if err := store.Put(source); err != nil {
		t.Fatalf("Put source: %v", err)
	}

	related, err := store.Related(target.Id)
	if err != nil {
		t.Fatalf("Related: %v", err)
	}
	if len(related) != 1 || related[0].Id != source.Id {
		t.Fatalf("expected [%s], got %+v", source.Id, related)
	}

	// Re-putting source with no relations must clear the stale edge.
	source.Relations = nil
	if err := store.Put(source); err != nil {
		t.Fatalf("Put source (cleared relations): %v", err)
	}
	related, err = store.Related(target.Id)
	if err != nil {
		t.Fatalf("Related after clear: %v", err)
	}
	if len(related) != 0 {
		t.Fatalf("expected no related records after clearing relations, got %+v", related)
	}
}

func TestSupersedeMarksRecordWithoutDeletingIt(t *testing.T) {
	store := newTestStore(t)

	rec := validRecord()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	newer := validRecord()
	newer.Id = "pkw:req/fixture/REQ-2"
	if err := store.Put(newer); err != nil {
		t.Fatalf("Put newer: %v", err)
	}

	if err := store.Supersede(rec.Id, newer.Id); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get after Supersede: %v", err)
	}
	if got.SupersededBy == nil || *got.SupersededBy != newer.Id {
		t.Fatalf("SupersededBy = %v, want %q", got.SupersededBy, newer.Id)
	}
	if got.Validity.State != protocol.KnowledgeRecordValidityStateSuperseded {
		t.Fatalf("Validity.State = %q, want superseded", got.Validity.State)
	}
}

func TestSupersedeReturnsErrNotFoundForMissingRecord(t *testing.T) {
	store := newTestStore(t)

	if err := store.Supersede("pkw:req/fixture/does-not-exist", "pkw:req/fixture/REQ-2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestProjectScopingPreventsLeakage mirrors taskstore's isolation test: two
// Stores over the same shared *storage.DB but different project ids must not
// see each other's records even when the record ids are identical, across
// Get, ListRecords and Related.
func TestProjectScopingPreventsLeakage(t *testing.T) {
	db := newTestDB(t)
	a := New(db, "project-a")
	b := New(db, "project-b")

	// Identical ids in both projects, so any leak would be a visible collision.
	recA := validRecord()
	recA.Title = "belongs to A"
	if err := a.Put(recA); err != nil {
		t.Fatalf("Put in A: %v", err)
	}

	// b has nothing under that id.
	if _, err := b.Get(recA.Id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("project B must not see project A's record, got err=%v", err)
	}

	// A's own Get sees it, with A's title.
	gotA, err := a.Get(recA.Id)
	if err != nil {
		t.Fatalf("Get in A: %v", err)
	}
	if gotA.Title != "belongs to A" {
		t.Fatalf("A's record title = %q, want %q", gotA.Title, "belongs to A")
	}

	// Now B puts a record with the same id but a distinct title and a relation.
	recB := validRecord()
	recB.Title = "belongs to B"
	target := validRecord()
	target.Id = "pkw:req/fixture/REQ-target"
	if err := b.Put(target); err != nil {
		t.Fatalf("Put target in B: %v", err)
	}
	recB.Relations = []protocol.KnowledgeRecordRelationsElem{
		{Type: protocol.KnowledgeRecordRelationsElemTypeDependsOn, Target: target.Id},
	}
	if err := b.Put(recB); err != nil {
		t.Fatalf("Put in B: %v", err)
	}

	// Each project's Get returns its own record under the shared id.
	gotA, err = a.Get(recA.Id)
	if err != nil {
		t.Fatalf("Get in A after B write: %v", err)
	}
	if gotA.Title != "belongs to A" {
		t.Fatalf("A's record leaked B's data: title = %q", gotA.Title)
	}
	gotB, err := b.Get(recB.Id)
	if err != nil {
		t.Fatalf("Get in B: %v", err)
	}
	if gotB.Title != "belongs to B" {
		t.Fatalf("B's record title = %q, want %q", gotB.Title, "belongs to B")
	}

	// ListRecords is scoped: A sees exactly its one record, B sees its two.
	listA, _, err := a.ListRecords(context.Background(), KnowledgeListQuery{})
	if err != nil {
		t.Fatalf("ListRecords A: %v", err)
	}
	if len(listA) != 1 {
		t.Fatalf("project A ListRecords = %d, want 1", len(listA))
	}
	listB, _, err := b.ListRecords(context.Background(), KnowledgeListQuery{})
	if err != nil {
		t.Fatalf("ListRecords B: %v", err)
	}
	if len(listB) != 2 {
		t.Fatalf("project B ListRecords = %d, want 2", len(listB))
	}

	// Related is scoped: B's relation must not surface for A, and A (which has
	// no target id at all) sees nothing related.
	relB, err := b.Related(target.Id)
	if err != nil {
		t.Fatalf("Related in B: %v", err)
	}
	if len(relB) != 1 || relB[0].Id != recB.Id {
		t.Fatalf("B Related = %+v, want the single edge from recB", relB)
	}
	relA, err := a.Related(target.Id)
	if err != nil {
		t.Fatalf("Related in A: %v", err)
	}
	if len(relA) != 0 {
		t.Fatalf("A Related = %+v, want none (B's edge must not leak)", relA)
	}
}
