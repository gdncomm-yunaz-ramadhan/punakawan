package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// planTestStores opens a fresh storage kernel and returns both stores
// plan_handler.go needs, plus the one registered delivery project
// ("proj-a") its tests seed plans and links against.
func planTestStores(t *testing.T) (*delivery.Store, *plan.Store, *protocol.DeliveryProject) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	deliveries := delivery.NewStore(db)
	plans := plan.NewStore(db)
	project, err := deliveries.RegisterProject(context.Background(), "reg-proj-a", delivery.NewID(), "proj-a", "https://example.test/proj-a.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	return deliveries, plans, project
}

func TestListPlansHandlerReturnsSummariesWithLinkedDeliveries(t *testing.T) {
	deliveries, plans, project := planTestStores(t)
	ctx := context.Background()

	planA, err := plans.Save(ctx, plan.Plan{ID: "plan-a", Objective: "Plan A", ProjectIDs: []string{project.Id}})
	if err != nil {
		t.Fatalf("Save plan-a: %v", err)
	}
	if _, err := plans.Save(ctx, plan.Plan{ID: "plan-b", Objective: "Plan B", ProjectIDs: []string{project.Id}}); err != nil {
		t.Fatalf("Save plan-b: %v", err)
	}

	orch, err := deliveries.CreateOrchestration(ctx, "create-d1", "d1", nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if _, err := deliveries.AttachProject(ctx, "attach-d1", orch.Id, orch.Revision, project.Id); err != nil {
		t.Fatalf("AttachProject: %v", err)
	}
	if err := deliveries.LinkProjectPlan(ctx, "link-d1-plan-a", orch.Id, project.Id, planA.ID, planA.Revision); err != nil {
		t.Fatalf("LinkProjectPlan: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/plans", nil)
	req.SetPathValue("projectId", "proj-a")
	rec := httptest.NewRecorder()
	ListPlansHandler(deliveries, plans)(rec, req)

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
	a := body.Items[0]
	if a.ID != "plan-a" || a.Objective != "Plan A" || a.CurrentRevision != 1 {
		t.Fatalf("plan-a summary = %+v", a)
	}
	if len(a.LinkedDeliveries) != 1 || a.LinkedDeliveries[0].OrchestrationID != "d1" {
		t.Fatalf("plan-a linked_deliveries = %+v, want one link to d1", a.LinkedDeliveries)
	}
	b := body.Items[1]
	if b.ID != "plan-b" || len(b.LinkedDeliveries) != 0 {
		t.Fatalf("plan-b summary = %+v, want no linked deliveries", b)
	}
}

func TestListPlansHandlerUnknownProjectServesEmptyList(t *testing.T) {
	deliveries, plans, _ := planTestStores(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nope/plans", nil)
	req.SetPathValue("projectId", "nope")
	rec := httptest.NewRecorder()
	ListPlansHandler(deliveries, plans)(rec, req)
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
		t.Fatalf("items = %+v, want none for a project with no matching delivery project", body.Items)
	}
}

func TestListPlansHandlerEmptyProject(t *testing.T) {
	deliveries, plans, _ := planTestStores(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/plans", nil)
	req.SetPathValue("projectId", "proj-a")
	rec := httptest.NewRecorder()
	ListPlansHandler(deliveries, plans)(rec, req)
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

func TestPlanHandlerReturnsHeadRevisionByDefault(t *testing.T) {
	deliveries, plans, project := planTestStores(t)
	ctx := context.Background()
	if _, err := plans.Save(ctx, plan.Plan{ID: "plan-a", Objective: "v1", ProjectIDs: []string{project.Id}}); err != nil {
		t.Fatalf("Save v1: %v", err)
	}
	if _, err := plans.Save(ctx, plan.Plan{ID: "plan-a", Objective: "v2", ProjectIDs: []string{project.Id}}); err != nil {
		t.Fatalf("Save v2: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/plans/plan-a", nil)
	req.SetPathValue("projectId", "proj-a")
	req.SetPathValue("planId", "plan-a")
	rec := httptest.NewRecorder()
	PlanHandler(deliveries, plans)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp planDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plan.Revision != 2 || resp.Plan.Objective != "v2" {
		t.Fatalf("plan = %+v, want head revision 2 (v2)", resp.Plan)
	}
}

// TestPlanHandlerRendersExactRevisionWhenRequested covers the exact
// scenario a delivery's own plan link must never lose: a delivery links
// revision 2 of a plan whose lineage has since moved to revision 3, and
// asking for that plan through this exact delivery's link (?revision=2)
// must still render revision 2, not silently substitute the head.
func TestPlanHandlerRendersExactRevisionWhenRequested(t *testing.T) {
	deliveries, plans, project := planTestStores(t)
	ctx := context.Background()
	if _, err := plans.Save(ctx, plan.Plan{ID: "plan-a", Objective: "v1", ProjectIDs: []string{project.Id}}); err != nil {
		t.Fatalf("Save v1: %v", err)
	}
	v2, err := plans.Save(ctx, plan.Plan{ID: "plan-a", Objective: "v2", ProjectIDs: []string{project.Id}})
	if err != nil {
		t.Fatalf("Save v2: %v", err)
	}
	if _, err := plans.Save(ctx, plan.Plan{ID: "plan-a", Objective: "v3", ProjectIDs: []string{project.Id}}); err != nil {
		t.Fatalf("Save v3: %v", err)
	}

	orch, err := deliveries.CreateOrchestration(ctx, "create-d1", "d1", nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if _, err := deliveries.AttachProject(ctx, "attach-d1", orch.Id, orch.Revision, project.Id); err != nil {
		t.Fatalf("AttachProject: %v", err)
	}
	if err := deliveries.LinkProjectPlan(ctx, "link-d1-v2", orch.Id, project.Id, v2.ID, v2.Revision); err != nil {
		t.Fatalf("LinkProjectPlan: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/plans/plan-a?revision=2", nil)
	req.SetPathValue("projectId", "proj-a")
	req.SetPathValue("planId", "plan-a")
	rec := httptest.NewRecorder()
	PlanHandler(deliveries, plans)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var resp planDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plan.Revision != 2 || resp.Plan.Objective != "v2" {
		t.Fatalf("plan = %+v, want exact revision 2 (v2), not the head", resp.Plan)
	}
	if len(resp.LinkedDeliveries) != 1 || resp.LinkedDeliveries[0].PlanRevision != 2 {
		t.Fatalf("linked_deliveries = %+v, want one link naming revision 2", resp.LinkedDeliveries)
	}
}

func TestPlanHandlerUnknownPlan404(t *testing.T) {
	deliveries, plans, _ := planTestStores(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a/plans/ghost", nil)
	req.SetPathValue("projectId", "proj-a")
	req.SetPathValue("planId", "ghost")
	rec := httptest.NewRecorder()
	PlanHandler(deliveries, plans)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestPlanHandlerUnknownProject404(t *testing.T) {
	deliveries, plans, _ := planTestStores(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nope/plans/plan-a", nil)
	req.SetPathValue("projectId", "nope")
	req.SetPathValue("planId", "plan-a")
	rec := httptest.NewRecorder()
	PlanHandler(deliveries, plans)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
