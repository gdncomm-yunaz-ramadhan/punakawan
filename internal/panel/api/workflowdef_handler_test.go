package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/workflowdef"
)

// wfTestHandlers builds a WorkflowDefHandlers bound to a single temp root,
// with a capability set covering the MCP tools plus two adapter ops, and a
// RunCreator that records its last call. resolveRoot maps "proj" -> root and
// errors for anything else (to exercise 404s).
func wfTestHandlers(t *testing.T) (*WorkflowDefHandlers, string, *fakeRunCreator) {
	t.Helper()
	root := t.TempDir()
	caps := workflowdef.NewCapabilitySet(workflowdef.KnownMCPCapabilities(), []string{"knowledge.search", "jira.issue.search"})
	frc := &fakeRunCreator{runID: "run-xyz"}
	resolve := func(projectID string) (string, error) {
		if projectID != "proj" {
			return "", errUnknownProject
		}
		return root, nil
	}
	newInvoker := func(projectID, r string) workflowdef.Invoker {
		return workflowdef.NewInvoker(caps, frc.create)
	}
	return NewWorkflowDefHandlers(resolve, caps, newInvoker), root, frc
}

var errUnknownProject = &wfErr{"unknown project"}

type wfErr struct{ s string }

func (e *wfErr) Error() string { return e.s }

type fakeRunCreator struct {
	runID    string
	err      error
	called   bool
	gotDef   workflowdef.Definition
	gotInput map[string]any
}

func (f *fakeRunCreator) create(_ context.Context, d workflowdef.Definition, in map[string]any) (string, error) {
	f.called = true
	f.gotDef = d
	f.gotInput = in
	return f.runID, f.err
}

func wfBody(t *testing.T) []byte {
	t.Helper()
	def := workflowdef.Definition{
		Version:             workflowdef.SchemaVersion,
		ID:                  "feature-delivery",
		Name:                "Feature Delivery",
		Enabled:             true,
		Steps:               []workflowdef.Step{{ID: "s1", Capability: "knowledge.search"}},
		AllowedCapabilities: []string{"knowledge.search"},
	}
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func req(method, path string, body []byte, pv map[string]string) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	for k, v := range pv {
		r.SetPathValue(k, v)
	}
	return r
}

func TestWorkflowCreateAndGetAndList(t *testing.T) {
	h, _, _ := wfTestHandlers(t)

	// Create.
	rec := httptest.NewRecorder()
	h.Create()(rec, req(http.MethodPost, "/x", wfBody(t), map[string]string{"projectId": "proj"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body)
	}
	var created workflowdef.Definition
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Revision != 1 {
		t.Fatalf("created revision = %d, want 1", created.Revision)
	}

	// Get.
	rec = httptest.NewRecorder()
	h.Get()(rec, req(http.MethodGet, "/x", nil, map[string]string{"projectId": "proj", "workflowId": "feature-delivery"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}

	// List.
	rec = httptest.NewRecorder()
	h.List()(rec, req(http.MethodGet, "/x", nil, map[string]string{"projectId": "proj"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var listOut struct {
		Items []workflowdef.Definition `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &listOut)
	if len(listOut.Items) != 1 || listOut.Items[0].ID != "feature-delivery" {
		t.Fatalf("list = %+v", listOut)
	}
}

func TestWorkflowCreateUnknownCapability400(t *testing.T) {
	h, _, _ := wfTestHandlers(t)
	def := workflowdef.Definition{
		Version:             workflowdef.SchemaVersion,
		ID:                  "bad",
		Name:                "Bad",
		Steps:               []workflowdef.Step{{ID: "s1", Capability: "no.such.cap"}},
		AllowedCapabilities: []string{"no.such.cap"},
	}
	raw, _ := json.Marshal(def)
	rec := httptest.NewRecorder()
	h.Create()(rec, req(http.MethodPost, "/x", raw, map[string]string{"projectId": "proj"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var out map[string]string
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out["code"] != "unknown_capability" {
		t.Fatalf("code = %q, want unknown_capability", out["code"])
	}
}

func TestWorkflowCreateCommandNotAllowed400(t *testing.T) {
	h, _, _ := wfTestHandlers(t)
	def := workflowdef.Definition{
		Version: workflowdef.SchemaVersion,
		ID:      "bad",
		Name:    "Bad",
		Steps:   []workflowdef.Step{{ID: "s1", Capability: "rm -rf /"}},
	}
	raw, _ := json.Marshal(def)
	rec := httptest.NewRecorder()
	h.Create()(rec, req(http.MethodPost, "/x", raw, map[string]string{"projectId": "proj"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var out map[string]string
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out["code"] != "command_not_allowed" {
		t.Fatalf("code = %q, want command_not_allowed", out["code"])
	}
}

func TestWorkflowCreateRevisionConflict409(t *testing.T) {
	h, _, _ := wfTestHandlers(t)
	// First create -> rev 1.
	rec := httptest.NewRecorder()
	h.Create()(rec, req(http.MethodPost, "/x", wfBody(t), map[string]string{"projectId": "proj"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create = %d", rec.Code)
	}
	// Re-create the same id with Revision still 0 (stale) -> conflict.
	rec = httptest.NewRecorder()
	h.Create()(rec, req(http.MethodPost, "/x", wfBody(t), map[string]string{"projectId": "proj"}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body)
	}
}

func TestWorkflowGet404(t *testing.T) {
	h, _, _ := wfTestHandlers(t)
	rec := httptest.NewRecorder()
	h.Get()(rec, req(http.MethodGet, "/x", nil, map[string]string{"projectId": "proj", "workflowId": "ghost"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestWorkflowUnknownProject404(t *testing.T) {
	h, _, _ := wfTestHandlers(t)
	rec := httptest.NewRecorder()
	h.List()(rec, req(http.MethodGet, "/x", nil, map[string]string{"projectId": "other"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestWorkflowEnableDisable(t *testing.T) {
	h, _, _ := wfTestHandlers(t)
	// Create enabled.
	rec := httptest.NewRecorder()
	h.Create()(rec, req(http.MethodPost, "/x", wfBody(t), map[string]string{"projectId": "proj"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}

	pv := map[string]string{"projectId": "proj", "workflowId": "feature-delivery"}

	rec = httptest.NewRecorder()
	h.Disable()(rec, req(http.MethodPost, "/x", nil, pv))
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d", rec.Code)
	}
	var d workflowdef.Definition
	json.Unmarshal(rec.Body.Bytes(), &d)
	if d.Enabled {
		t.Fatalf("still enabled after disable")
	}

	rec = httptest.NewRecorder()
	h.Enable()(rec, req(http.MethodPost, "/x", nil, pv))
	if rec.Code != http.StatusOK {
		t.Fatalf("enable = %d", rec.Code)
	}
	json.Unmarshal(rec.Body.Bytes(), &d)
	if !d.Enabled {
		t.Fatalf("not enabled after enable")
	}

	// Enable on unknown id -> 404.
	rec = httptest.NewRecorder()
	h.Enable()(rec, req(http.MethodPost, "/x", nil, map[string]string{"projectId": "proj", "workflowId": "ghost"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("enable ghost = %d, want 404", rec.Code)
	}
}

func TestWorkflowInvoke(t *testing.T) {
	h, _, frc := wfTestHandlers(t)
	// Create enabled definition.
	rec := httptest.NewRecorder()
	h.Create()(rec, req(http.MethodPost, "/x", wfBody(t), map[string]string{"projectId": "proj"}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}

	pv := map[string]string{"projectId": "proj", "workflowId": "feature-delivery"}
	body := []byte(`{"inputs":{"branch":"main"}}`)
	rec = httptest.NewRecorder()
	h.Invoke()(rec, req(http.MethodPost, "/x", body, pv))
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke = %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		RunID string `json:"run_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.RunID != "run-xyz" {
		t.Fatalf("run_id = %q", out.RunID)
	}
	if !frc.called || frc.gotInput["branch"] != "main" {
		t.Fatalf("RunCreator not invoked with inputs: %+v", frc)
	}
}

func TestWorkflowInvokeDisabled(t *testing.T) {
	h, root, frc := wfTestHandlers(t)
	// Seed a disabled definition directly in the store.
	store, _ := workflowdef.Open(root)
	if _, err := store.Save(workflowdef.Definition{
		Version:             workflowdef.SchemaVersion,
		ID:                  "off",
		Name:                "Off",
		Enabled:             false,
		Steps:               []workflowdef.Step{{ID: "s1", Capability: "knowledge.search"}},
		AllowedCapabilities: []string{"knowledge.search"},
	}); err != nil {
		t.Fatal(err)
	}

	pv := map[string]string{"projectId": "proj", "workflowId": "off"}
	rec := httptest.NewRecorder()
	h.Invoke()(rec, req(http.MethodPost, "/x", []byte(`{"inputs":{}}`), pv))
	if rec.Code != http.StatusConflict {
		t.Fatalf("invoke disabled = %d, want 409: %s", rec.Code, rec.Body)
	}
	if frc.called {
		t.Fatalf("RunCreator must not run for disabled def")
	}
}

func TestWorkflowInvoke404(t *testing.T) {
	h, _, _ := wfTestHandlers(t)
	rec := httptest.NewRecorder()
	h.Invoke()(rec, req(http.MethodPost, "/x", []byte(`{"inputs":{}}`), map[string]string{"projectId": "proj", "workflowId": "ghost"}))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("invoke ghost = %d, want 404", rec.Code)
	}
}
