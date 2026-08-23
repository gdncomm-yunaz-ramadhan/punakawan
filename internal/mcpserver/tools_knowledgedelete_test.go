package mcpserver

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func putFixtureRecord(t *testing.T, store *knowledge.Store, id, title string, scope *protocol.KnowledgeRecordScope) protocol.KnowledgeRecord {
	t.Helper()
	rec := protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/" + id,
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  title,
		Scope:  scope,
		Source: protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put %s: %v", id, err)
	}
	return rec
}

func TestDeleteKnowledgeRemovesRecordsAndReportsMissingIds(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := putFixtureRecord(t, store, "REQ-1", "Stale finding", nil)

	var out map[string]any
	callTool(t, cs, "delete_knowledge", map[string]any{
		"ids": []string{rec.Id, "pkw:req/fixture/does-not-exist"},
	}, &out)

	deleted, _ := out["deleted"].([]any)
	if len(deleted) != 1 || deleted[0] != rec.Id {
		t.Fatalf("deleted = %v, want [%s]", deleted, rec.Id)
	}
	notFound, _ := out["not_found"].([]any)
	if len(notFound) != 1 || notFound[0] != "pkw:req/fixture/does-not-exist" {
		t.Fatalf("not_found = %v, want [pkw:req/fixture/does-not-exist]", notFound)
	}

	if _, err := store.Get(rec.Id); err == nil {
		t.Fatal("expected the record to be gone from the store after delete_knowledge")
	}
}

// TestDeleteKnowledgeCommitsToDoltAndIsRevertable checks the commit_hash
// contract at the MCP boundary (non-empty, distinct per call); the deeper
// guarantee - that AS OF '<hash>~1' / `dolt checkout <hash>~1` actually
// restores the pre-delete row - is proven once, precisely, in
// internal/knowledge's own white-box test (commit_test.go), which can reach
// the store's raw SQL connection. Re-deriving that here would mean punching
// a raw-SQL hole through the store's boundary for no added coverage.
func TestDeleteKnowledgeCommitsToDoltAndIsRevertable(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := putFixtureRecord(t, store, "REQ-5", "Commit-tracked finding", nil)

	var out map[string]any
	callTool(t, cs, "delete_knowledge", map[string]any{
		"ids": []string{rec.Id},
	}, &out)

	hash, _ := out["commit_hash"].(string)
	if hash == "" {
		t.Fatalf("expected a non-empty commit_hash after an actual delete, got %+v", out)
	}
}

func TestDeleteKnowledgeOmitsCommitHashWhenNothingDeleted(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "delete_knowledge", map[string]any{
		"ids": []string{"pkw:req/fixture/does-not-exist"},
	}, &out)

	if hash, ok := out["commit_hash"]; ok && hash != "" {
		t.Fatalf("expected no commit_hash when every id was not_found, got %+v", out)
	}
}

func strp(s string) *string { return &s }
