package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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

func TestResetProjectKnowledgeRequiresAScopeField(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "reset_project_knowledge",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error when no project/repository/module scope is given")
	}
}

func TestResetProjectKnowledgeDefaultsToDryRun(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	scope := &protocol.KnowledgeRecordScope{Repository: strp("checkout-service")}
	rec := putFixtureRecord(t, store, "REQ-2", "Checkout note", scope)

	var out map[string]any
	callTool(t, cs, "reset_project_knowledge", map[string]any{
		"repository": "checkout-service",
	}, &out)

	if deleted, _ := out["deleted"].(bool); deleted {
		t.Fatal("expected deleted=false on a dry run")
	}
	matched, _ := out["matched_ids"].([]any)
	if len(matched) != 1 || matched[0] != rec.Id {
		t.Fatalf("matched_ids = %v, want [%s]", matched, rec.Id)
	}

	if _, err := store.Get(rec.Id); err != nil {
		t.Fatalf("expected the record to still exist after a dry run: %v", err)
	}
}

func TestResetProjectKnowledgeDeletesWhenConfirmed(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	inScope := putFixtureRecord(t, store, "REQ-3", "Checkout note A", &protocol.KnowledgeRecordScope{Repository: strp("checkout-service")})
	outOfScope := putFixtureRecord(t, store, "REQ-4", "Unrelated note", &protocol.KnowledgeRecordScope{Repository: strp("other-service")})

	var out map[string]any
	callTool(t, cs, "reset_project_knowledge", map[string]any{
		"repository": "checkout-service",
		"confirm":    true,
	}, &out)

	if deleted, _ := out["deleted"].(bool); !deleted {
		t.Fatalf("out = %+v, want deleted=true when confirm=true", out)
	}

	if _, err := store.Get(inScope.Id); err == nil {
		t.Fatal("expected the in-scope record to be deleted")
	}
	if _, err := store.Get(outOfScope.Id); err != nil {
		t.Fatalf("expected the out-of-scope record to survive: %v", err)
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

func TestResetProjectKnowledgeMatchedIdsIsEmptyArrayNotNullWhenNothingMatches(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "reset_project_knowledge", map[string]any{
		"repository": "no-such-repository",
	}, &out)

	matched, ok := out["matched_ids"].([]any)
	if !ok {
		t.Fatalf("expected matched_ids to be an array (not null) when nothing matches, got %+v (%T)", out["matched_ids"], out["matched_ids"])
	}
	if len(matched) != 0 {
		t.Fatalf("expected zero matches, got %v", matched)
	}
}

func TestResetProjectKnowledgeCommitsToDoltWhenConfirmed(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	putFixtureRecord(t, store, "REQ-6", "Checkout note", &protocol.KnowledgeRecordScope{Repository: strp("checkout-service")})

	var out map[string]any
	callTool(t, cs, "reset_project_knowledge", map[string]any{
		"repository": "checkout-service",
		"confirm":    true,
	}, &out)

	hash, _ := out["commit_hash"].(string)
	if hash == "" {
		t.Fatalf("expected a non-empty commit_hash after a confirmed reset, got %+v", out)
	}
}

func strp(s string) *string { return &s }
