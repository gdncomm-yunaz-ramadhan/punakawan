package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ygrip/punakawan/internal/impact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeImpactReader delegates to the real internal/impact store rooted at a temp
// dir so QueryImpact exercises the genuine BFS engine.
type fakeImpactReader struct{ root string }

func (f *fakeImpactReader) ImpactNodes(ctx context.Context, projectID string) ([]protocol.ImpactNode, error) {
	return impact.Nodes(f.root)
}

func (f *fakeImpactReader) ImpactNode(ctx context.Context, projectID, nodeID string) (protocol.ImpactNode, bool, error) {
	return impact.GetNode(f.root, nodeID)
}

func (f *fakeImpactReader) QueryImpact(ctx context.Context, projectID, subjectID string, depth int, include []string) (impact.ImpactResult, error) {
	return impact.Query(f.root, subjectID, depth, include)
}

func (f *fakeImpactReader) RefreshImpact(ctx context.Context, projectID string) error {
	return impact.Refresh(f.root)
}

// seedImpactGraph writes two source symbols A and B with a calls edge A->B, so
// a query from A at depth 1 reports B as direct impact.
func seedImpactGraph(t *testing.T, root string) {
	t.Helper()
	for _, id := range []string{"sym:A", "sym:B"} {
		if err := impact.UpsertNode(root, protocol.ImpactNode{Id: id, Type: protocol.ImpactNodeTypeSourceSymbol}); err != nil {
			t.Fatalf("seed node %s: %v", id, err)
		}
	}
	if err := impact.UpsertEdge(root, protocol.ImpactEdge{
		From:       "sym:A",
		To:         "sym:B",
		Type:       protocol.ImpactEdgeTypeCalls,
		Confidence: protocol.ImpactEdgeConfidenceObserved,
	}); err != nil {
		t.Fatalf("seed edge: %v", err)
	}
}

func TestImpactQueryReturnsResult(t *testing.T) {
	root := t.TempDir()
	seedImpactGraph(t, root)
	reader := &fakeImpactReader{root: root}

	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/impact/query",
		`{"subject_id":"sym:A","depth":2}`,
		map[string]string{"projectId": "proj-a"}, ImpactQueryHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var result impact.ImpactResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.DirectImpact) != 1 || result.DirectImpact[0].Id != "sym:B" {
		t.Fatalf("direct impact = %+v, want [sym:B]", result.DirectImpact)
	}
}

func TestImpactQueryMissingSubject400(t *testing.T) {
	reader := &fakeImpactReader{root: t.TempDir()}
	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/impact/query", `{"depth":1}`,
		map[string]string{"projectId": "proj-a"}, ImpactQueryHandler(reader))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestImpactNodesListAndSingle(t *testing.T) {
	root := t.TempDir()
	seedImpactGraph(t, root)
	reader := &fakeImpactReader{root: root}

	rec := doReq(t, http.MethodGet, "/api/v1/projects/proj-a/impact/nodes", "",
		map[string]string{"projectId": "proj-a"}, ImpactNodesHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("nodes status = %d, want 200", rec.Code)
	}
	var list struct {
		Items []protocol.ImpactNode `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(list.Items))
	}

	rec = doReq(t, http.MethodGet, "/api/v1/projects/proj-a/impact/nodes/sym:A", "",
		map[string]string{"projectId": "proj-a", "nodeId": "sym:A"}, ImpactNodeHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("node status = %d, want 200", rec.Code)
	}

	rec = doReq(t, http.MethodGet, "/api/v1/projects/proj-a/impact/nodes/nope", "",
		map[string]string{"projectId": "proj-a", "nodeId": "nope"}, ImpactNodeHandler(reader))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing node status = %d, want 404", rec.Code)
	}
}
