package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
)

// planTestStores builds a ProjectStores over a single temp-dir-backed
// project and returns both the resolver-backed stores and the PlanStore
// used to seed content.
func planTestStores(t *testing.T) (*artifact.ProjectStores, *artifact.PlanStore) {
	t.Helper()
	root := t.TempDir()
	resolver := func(projectID string) (string, error) {
		if projectID != "proj-a" {
			return "", fmt.Errorf("unknown project %q", projectID)
		}
		return root, nil
	}
	return artifact.NewProjectStores(resolver), &artifact.PlanStore{WorkspaceRoot: root}
}

func TestListPlansHandlerReturnsSummaries(t *testing.T) {
	stores, seed := planTestStores(t)

	// One plan with an explicit manifest, one manifest-less (synthesized).
	if err := seed.SaveManifest("plan-a", &artifact.PlanManifest{
		Title:        "Plan A",
		Status:       artifact.PlanStatusProposed,
		RelatedTasks: []string{"punokawan-sv8"},
		DerivedFrom:  artifact.Derivations{Knowledge: []string{"k1"}},
	}); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if _, err := seed.CreateVersion("plan-a", "proj-a", []byte("# a"), time.Now()); err != nil {
		t.Fatalf("CreateVersion(plan-a): %v", err)
	}
	if _, err := seed.CreateVersion("plan-b", "proj-a", []byte("# b"), time.Now()); err != nil {
		t.Fatalf("CreateVersion(plan-b): %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/plans", nil)
	req.SetPathValue("projectId", "proj-a")
	rec := httptest.NewRecorder()
	ListPlansHandler(stores)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []PlanSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %+v, want 2", body.Items)
	}
	// Sorted by id: plan-a then plan-b.
	a := body.Items[0]
	if a.ID != "plan-a" || a.Title != "Plan A" || a.Status != artifact.PlanStatusProposed {
		t.Fatalf("plan-a summary = %+v", a)
	}
	if a.CurrentVersion != 1 {
		t.Fatalf("plan-a current_version = %d, want 1 (manifest bumped by CreateVersion)", a.CurrentVersion)
	}
	if len(a.RelatedTasks) != 1 || a.RelatedTasks[0] != "punokawan-sv8" {
		t.Fatalf("plan-a related_tasks = %v", a.RelatedTasks)
	}
	b := body.Items[1]
	if b.ID != "plan-b" || b.Status != artifact.PlanStatusDraft {
		t.Fatalf("plan-b summary = %+v, want synthesized draft", b)
	}
}

func TestListPlansHandlerUnknownProject404(t *testing.T) {
	stores, _ := planTestStores(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nope/plans", nil)
	req.SetPathValue("projectId", "nope")
	rec := httptest.NewRecorder()
	ListPlansHandler(stores)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestListPlansHandlerEmptyProject(t *testing.T) {
	stores, _ := planTestStores(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/plans", nil)
	req.SetPathValue("projectId", "proj-a")
	rec := httptest.NewRecorder()
	ListPlansHandler(stores)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []PlanSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("items = %+v, want empty", body.Items)
	}
}

func TestPlanHandlerReturnsDetailWithContent(t *testing.T) {
	stores, seed := planTestStores(t)
	if _, err := seed.CreateVersion("plan-a", "proj-a", []byte("# hello v1"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	if _, err := seed.CreateVersion("plan-a", "proj-a", []byte("# hello v2"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/plans/plan-a", nil)
	req.SetPathValue("projectId", "proj-a")
	req.SetPathValue("planId", "plan-a")
	rec := httptest.NewRecorder()
	PlanHandler(stores)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp planDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Manifest == nil || resp.Manifest.ID != "plan-a" {
		t.Fatalf("manifest = %+v", resp.Manifest)
	}
	if resp.Manifest.CurrentVersion != 2 {
		t.Fatalf("current_version = %d, want 2", resp.Manifest.CurrentVersion)
	}
	if resp.CurrentVersionContent == nil || *resp.CurrentVersionContent != "# hello v2" {
		t.Fatalf("current_version_content = %v, want %q", resp.CurrentVersionContent, "# hello v2")
	}
}

func TestPlanHandlerUnknownPlan404(t *testing.T) {
	stores, _ := planTestStores(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/plans/ghost", nil)
	req.SetPathValue("projectId", "proj-a")
	req.SetPathValue("planId", "ghost")
	rec := httptest.NewRecorder()
	PlanHandler(stores)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestPlanHandlerUnknownProject404(t *testing.T) {
	stores, _ := planTestStores(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nope/plans/plan-a", nil)
	req.SetPathValue("projectId", "nope")
	req.SetPathValue("planId", "plan-a")
	rec := httptest.NewRecorder()
	PlanHandler(stores)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
