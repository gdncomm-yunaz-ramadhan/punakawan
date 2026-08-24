package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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

// TestSearchKnowledgeSourceLocalMatchesOmittedSource guards §7.4's backward
// compatibility requirement: explicitly passing source=local must return the
// same shape (including the tagged source field) as omitting it entirely.
func TestSearchKnowledgeSourceLocalMatchesOmittedSource(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := putFixtureRecord(t, store, "SRC-1", "distinctive-marker-falcon", nil)

	var omitted, explicit map[string]any
	callTool(t, cs, "search_knowledge", map[string]any{"query": "distinctive-marker-falcon"}, &omitted)
	callTool(t, cs, "search_knowledge", map[string]any{"query": "distinctive-marker-falcon", "source": "local"}, &explicit)

	omittedResults, _ := omitted["results"].([]any)
	explicitResults, _ := explicit["results"].([]any)
	if len(omittedResults) != 1 || len(explicitResults) != 1 {
		t.Fatalf("omitted = %v, explicit = %v, want exactly one hit each", omitted, explicit)
	}
	first := explicitResults[0].(map[string]any)
	if first["source"] != "local" {
		t.Fatalf("results[0].source = %v, want local", first["source"])
	}
	if first["id"] != rec.Id {
		t.Fatalf("results[0].id = %v, want %s", first["id"], rec.Id)
	}
}

// TestSearchKnowledgeSourceMomReturnsAnErrorNotFakeData guards the chosen
// stub policy for a directly-requested provider (as opposed to source=all's
// fan-out): Mom has no working backend yet, so asking for it explicitly must
// fail loudly (§15) rather than silently returning zero results.
func TestSearchKnowledgeSourceMomReturnsAnErrorNotFakeData(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_knowledge",
		Arguments: map[string]any{"query": "anything", "source": "mom"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected an error result for an unconfigured mom provider, got %+v", res.StructuredContent)
	}
}

// TestSearchKnowledgeSourceAllSurfacesStubFailuresWithoutLosingLocalResults
// is §7.4's federated fan-out: an unconfigured Mom/Codepedia must not abort
// the whole search, and the local result that did succeed keeps its own
// source/ref rather than being normalized away.
func TestSearchKnowledgeSourceAllSurfacesStubFailuresWithoutLosingLocalResults(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := putFixtureRecord(t, store, "SRC-2", "distinctive-marker-egret", nil)

	var out map[string]any
	callTool(t, cs, "search_knowledge", map[string]any{"query": "distinctive-marker-egret", "source": "all"}, &out)

	results, _ := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v, want exactly the one local hit", results)
	}
	first := results[0].(map[string]any)
	if first["source"] != "local" || first["id"] != rec.Id {
		t.Fatalf("results[0] = %v, want source=local id=%s", first, rec.Id)
	}

	providerErrors, ok := out["provider_errors"].([]any)
	if !ok || len(providerErrors) != 2 {
		t.Fatalf("provider_errors = %v, want one entry each for mom and codepedia", out["provider_errors"])
	}
	seen := map[string]bool{}
	for _, pe := range providerErrors {
		m := pe.(map[string]any)
		seen[m["source"].(string)] = true
		if !strings.Contains(m["error"].(string), "no backend configured") {
			t.Errorf("provider_errors entry %v does not mention being unconfigured", m)
		}
	}
	if !seen["mom"] || !seen["codepedia"] {
		t.Fatalf("provider_errors sources = %v, want mom and codepedia", providerErrors)
	}
}

// TestSearchKnowledgeRejectsUnknownSource guards the enum boundary: a typo'd
// source value must fail clearly rather than silently falling back to local.
func TestSearchKnowledgeRejectsUnknownSource(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_knowledge",
		Arguments: map[string]any{"query": "anything", "source": "bogus"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for an unknown source")
	}
}
