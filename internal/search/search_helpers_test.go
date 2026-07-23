package search

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func newTestStore(t *testing.T) *knowledge.Store {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}

	dir := t.TempDir()
	sup := tools.New(dir)
	store, err := knowledge.Open(sup, filepath.Join(dir, "knowledge"))
	if err != nil {
		t.Fatalf("knowledge.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return store
}

func newTestIndex(t *testing.T) *Index {
	t.Helper()
	ix, err := OpenIndex(filepath.Join(t.TempDir(), "bm25"))
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	t.Cleanup(func() {
		if err := ix.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return ix
}

func newRecord(t *testing.T, localID string) protocol.KnowledgeRecord {
	t.Helper()
	return protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/" + localID,
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "fixture record " + localID,
		Source: protocol.KnowledgeRecordSource{
			Provider:    "test",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateObserved,
		},
	}
}

func putAndIndex(t *testing.T, store *knowledge.Store, ix *Index, rec protocol.KnowledgeRecord) {
	t.Helper()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put %s: %v", rec.Id, err)
	}
	if err := ix.IndexRecord(knowledge.RecordWithUpdatedAt{Record: rec, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("IndexRecord %s: %v", rec.Id, err)
	}
}

func strp(s string) *string { return &s }
