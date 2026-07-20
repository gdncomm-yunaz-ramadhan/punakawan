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
			State: protocol.KnowledgeRecordValidityStateVerified,
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
