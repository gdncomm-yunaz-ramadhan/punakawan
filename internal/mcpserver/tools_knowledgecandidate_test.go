package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestKnowledgeRecordCandidatePersistsAndIsRetrievable(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "knowledge_record_candidate", map[string]any{
		"type":    "decision",
		"title":   "Use the facade for new knowledge callers",
		"content": "New code should depend on the knowledge facade, not knowledge.Store directly.",
	}, &out)

	ref, _ := out["ref"].(string)
	if ref == "" {
		t.Fatalf("out = %+v, want a non-empty ref", out)
	}
	rec, _ := out["record"].(map[string]any)
	if rec["id"] != ref {
		t.Fatalf("record.id = %v, want it to match ref %q", rec["id"], ref)
	}

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	got, err := store.Get(ref)
	if err != nil {
		t.Fatalf("Get(%q): %v", ref, err)
	}
	if got.Title != "Use the facade for new knowledge callers" {
		t.Errorf("got.Title = %q, want the submitted title", got.Title)
	}

	var searched map[string]any
	callTool(t, cs, "search_knowledge", map[string]any{"query": "facade for new knowledge callers"}, &searched)
	results, _ := searched["results"].([]any)
	if len(results) == 0 {
		t.Fatal("expected the candidate to be findable via search_knowledge once persisted")
	}
}

// TestKnowledgeRecordCandidateRequiresTitleAndContent checks that both
// required fields are actually enforced - a missing one is rejected either
// at the MCP schema-validation layer (a transport-level error, since
// Title/Content carry no `omitempty`) or, for a present-but-blank value, by
// the handler's own trimmed-empty check (an IsError result).
func TestKnowledgeRecordCandidateRequiresTitleAndContent(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	for _, args := range []map[string]any{
		{"type": "decision", "content": "missing title"},
		{"type": "decision", "title": "missing content"},
		{"type": "decision", "title": "  ", "content": "blank title"},
		{"type": "decision", "title": "blank content", "content": "  "},
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "knowledge_record_candidate",
			Arguments: args,
		})
		if err != nil {
			continue // schema validation rejected the missing required field before reaching the handler
		}
		if !res.IsError {
			t.Fatalf("args = %+v, want an error (schema rejection or IsError result)", args)
		}
	}
}

func TestKnowledgeRecordCandidateRejectsVerifiedWithoutVerifiedBy(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "knowledge_record_candidate",
		Arguments: map[string]any{
			"type":           "decision",
			"title":          "A claimed-verified candidate",
			"content":        "content",
			"validity_state": "verified",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for validity_state=verified without verified_by")
	}
}
