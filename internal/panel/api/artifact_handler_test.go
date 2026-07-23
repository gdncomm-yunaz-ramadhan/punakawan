package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/artifact"
)

func TestArtifactCurrentHandlerReturnsLatestContent(t *testing.T) {
	root := t.TempDir()
	plans := &artifact.PlanStore{WorkspaceRoot: root}
	seedPlan(t, plans, "plan-panel", "# v1\n")
	ref := seedPlan(t, plans, "plan-panel", "# v2\n")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/plan/plan-panel/current", nil)
	req.SetPathValue("type", "plan")
	req.SetPathValue("id", "plan-panel")
	rec := httptest.NewRecorder()
	ArtifactCurrentHandler(ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out artifactContentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Content != "# v2\n" || out.Reference.Version != ref.Version {
		t.Fatalf("out = %+v, want the latest version's content", out)
	}
}

func TestArtifactCurrentHandlerReturns404ForUnknownPlan(t *testing.T) {
	plans := &artifact.PlanStore{WorkspaceRoot: t.TempDir()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/plan/no-such-plan/current", nil)
	req.SetPathValue("type", "plan")
	req.SetPathValue("id", "no-such-plan")
	rec := httptest.NewRecorder()
	ArtifactCurrentHandler(ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestArtifactCurrentHandlerRejectsUnknownType(t *testing.T) {
	plans := &artifact.PlanStore{WorkspaceRoot: t.TempDir()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/retrieval_recipe/r-1/current", nil)
	req.SetPathValue("type", "retrieval_recipe")
	req.SetPathValue("id", "r-1")
	rec := httptest.NewRecorder()
	ArtifactCurrentHandler(ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
