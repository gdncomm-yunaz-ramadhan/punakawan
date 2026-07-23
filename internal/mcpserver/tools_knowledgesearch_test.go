package mcpserver

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestSearchKnowledgeReturnsBM25Matches(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/REQ-1",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Refund an approved order",
		Source: protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var out map[string]any
	callTool(t, cs, "search_knowledge", map[string]any{
		"query": "refund approved order",
	}, &out)

	results, ok := out["results"].([]any)
	if !ok || len(results) == 0 {
		t.Fatalf("out = %+v, want at least one result", out)
	}
	first := results[0].(map[string]any)
	if first["id"] != rec.Id {
		t.Fatalf("results[0].id = %v, want %s", first["id"], rec.Id)
	}
	match, _ := first["match"].(map[string]any)
	if match["kind"] != "bm25" {
		t.Fatalf("match.kind = %v, want bm25", match["kind"])
	}
	if _, ok := first["explanation"]; !ok {
		t.Fatal("expected an explanation field on the result, per §11.13")
	}
}

func TestSearchKnowledgeReturnsNoResultsForUnmatchedQuery(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "search_knowledge", map[string]any{
		"query": "nothing indexed matches this",
	}, &out)

	results, _ := out["results"].([]any)
	if len(results) != 0 {
		t.Fatalf("results = %v, want none against an empty knowledge store", results)
	}
}

func TestSearchKnowledgeRespectsTypeFilter(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	base := func(id, title string, typ protocol.KnowledgeRecordType) protocol.KnowledgeRecord {
		return protocol.KnowledgeRecord{
			Id:     "pkw:req/fixture/" + id,
			Type:   typ,
			Status: "active",
			Title:  title,
			Source: protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
			Extraction: protocol.KnowledgeRecordExtraction{
				Method: protocol.KnowledgeRecordExtractionMethodManual,
			},
			Validity: protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
		}
	}
	req := base("REQ-2", "Loyalty points expiry rule", protocol.KnowledgeRecordTypeRequirement)
	claim := base("CLAIM-1", "Loyalty points expiry rule assumption", protocol.KnowledgeRecordTypeClaim)
	if err := store.Put(req); err != nil {
		t.Fatalf("Put req: %v", err)
	}
	if err := store.Put(claim); err != nil {
		t.Fatalf("Put claim: %v", err)
	}

	var out map[string]any
	callTool(t, cs, "search_knowledge", map[string]any{
		"query": "loyalty points expiry rule",
		"types": []string{"requirement"},
	}, &out)

	results, _ := out["results"].([]any)
	for _, r := range results {
		if r.(map[string]any)["id"] == claim.Id {
			t.Fatalf("results = %v, want the claim excluded by the type filter", results)
		}
	}
	if len(results) == 0 {
		t.Fatal("expected the requirement to still match")
	}
}
