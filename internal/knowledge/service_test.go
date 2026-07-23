package knowledge

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "knowledge")
	sup := tools.New(dir)

	store, err := Open(sup, dataDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return store
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

func TestOpenReusesExistingDoltServer(t *testing.T) {
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "knowledge")
	sup := tools.New(dir)

	owner, err := Open(sup, dataDir)
	if err != nil {
		t.Fatalf("Open owner: %v", err)
	}
	shared, err := Open(sup, dataDir)
	if err != nil {
		_ = owner.Close()
		t.Fatalf("Open shared: %v", err)
	}
	if shared.server != nil {
		t.Fatal("expected the second store to reuse the existing server")
	}

	server := owner.server
	if err := owner.Close(); err != nil {
		t.Fatalf("Close owner: %v", err)
	}
	if err := shared.db.Ping(); err != nil {
		t.Fatalf("shared connection stopped with owner: %v", err)
	}
	if err := shared.Close(); err != nil {
		t.Fatalf("Close shared: %v", err)
	}
	if server != nil {
		_ = server.Stop()
	}
}
