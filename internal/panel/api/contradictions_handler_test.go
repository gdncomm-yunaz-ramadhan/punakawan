package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/contradiction"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeContradictionReader is a thin contract.ContradictionReader backed by the
// real stateless internal/contradiction store rooted at a temp dir, so the
// handlers' error mapping runs against the same ErrNotFound/ErrIllegalTransition
// the production ProjectSource returns. projectID is ignored (one root).
type fakeContradictionReader struct{ root string }

func (f *fakeContradictionReader) ListContradictions(ctx context.Context, projectID string) ([]protocol.Contradiction, error) {
	return contradiction.List(f.root)
}

func (f *fakeContradictionReader) GetContradiction(ctx context.Context, projectID, id string) (*protocol.Contradiction, error) {
	return contradiction.Get(f.root, id)
}

func (f *fakeContradictionReader) CreateContradiction(ctx context.Context, projectID string, c protocol.Contradiction) (*protocol.Contradiction, error) {
	if err := contradiction.Put(f.root, c, contradiction.PutOptions{}); err != nil {
		return nil, err
	}
	return contradiction.Get(f.root, c.Id)
}

func (f *fakeContradictionReader) ProposeContradictionResolution(ctx context.Context, projectID, id, proposed, rationale string, requiresHuman bool) (*protocol.Contradiction, error) {
	if err := contradiction.ProposeResolution(f.root, id, proposed, rationale, requiresHuman); err != nil {
		return nil, err
	}
	return contradiction.Get(f.root, id)
}

func (f *fakeContradictionReader) ResolveContradiction(ctx context.Context, projectID, id, statement, by string) (*protocol.Contradiction, error) {
	if err := contradiction.Resolve(f.root, id, statement, by); err != nil {
		return nil, err
	}
	return contradiction.Get(f.root, id)
}

func (f *fakeContradictionReader) AcceptContradictionDivergence(ctx context.Context, projectID, id, by string) (*protocol.Contradiction, error) {
	if err := contradiction.AcceptDivergence(f.root, id, by); err != nil {
		return nil, err
	}
	return contradiction.Get(f.root, id)
}

// seedContradiction writes a minimal valid contradiction (two claims satisfy
// the schema) at the given status.
func seedContradiction(t *testing.T, root, id string, status protocol.ContradictionStatus) {
	t.Helper()
	key := "payout.retry.max"
	c := protocol.Contradiction{
		Id:        id,
		ProjectId: "proj",
		Title:     "Disagreement about " + key,
		Severity:  protocol.ContradictionSeverityMaterial,
		Status:    status,
		Subject:   protocol.ContradictionSubject{Type: protocol.ContradictionSubjectTypeConfiguration, Key: &key},
		Claims: []protocol.ContradictionClaimsElem{
			{Source: protocol.ContradictionClaimsElemSource{Type: protocol.ContradictionClaimsElemSourceTypeRepository}, Statement: "A"},
			{Source: protocol.ContradictionClaimsElemSource{Type: protocol.ContradictionClaimsElemSourceTypeConfluence}, Statement: "B"},
		},
	}
	if err := contradiction.Put(root, c, contradiction.PutOptions{}); err != nil {
		t.Fatalf("seed contradiction: %v", err)
	}
}

func doReq(t *testing.T, method, target, body string, pathValues map[string]string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	for k, v := range pathValues {
		r.SetPathValue(k, v)
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

func TestContradictionListAndGet(t *testing.T) {
	root := t.TempDir()
	seedContradiction(t, root, "c1", protocol.ContradictionStatusDetected)
	reader := &fakeContradictionReader{root: root}

	rec := doReq(t, http.MethodGet, "/api/v1/projects/proj-a/contradictions", "",
		map[string]string{"projectId": "proj-a"}, ContradictionsListHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []protocol.Contradiction `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Id != "c1" {
		t.Fatalf("items = %+v, want one c1", list.Items)
	}

	rec = doReq(t, http.MethodGet, "/api/v1/projects/proj-a/contradictions/c1", "",
		map[string]string{"projectId": "proj-a", "id": "c1"}, ContradictionGetHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", rec.Code)
	}
}

func TestContradictionGetNotFound404(t *testing.T) {
	reader := &fakeContradictionReader{root: t.TempDir()}
	rec := doReq(t, http.MethodGet, "/api/v1/projects/proj-a/contradictions/missing", "",
		map[string]string{"projectId": "proj-a", "id": "missing"}, ContradictionGetHandler(reader))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestContradictionCreateAssignsIDWhenEmpty(t *testing.T) {
	reader := &fakeContradictionReader{root: t.TempDir()}
	body := `{"title":"T","project_id":"proj","severity":"material","status":"detected",` +
		`"subject":{"type":"configuration","key":"k"},` +
		`"claims":[{"source":{"type":"repository"},"statement":"A"},{"source":{"type":"confluence"},"statement":"B"}]}`
	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/contradictions", body,
		map[string]string{"projectId": "proj-a"}, ContradictionCreateHandler(reader))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Contradiction protocol.Contradiction `json:"contradiction"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Contradiction.Id == "" {
		t.Fatalf("server did not assign an id")
	}
	if out.Contradiction.Version != contradiction.Version {
		t.Fatalf("version = %q, want %q", out.Contradiction.Version, contradiction.Version)
	}
}

func TestContradictionResolveFlowHappyPath(t *testing.T) {
	root := t.TempDir()
	// ProposeResolution advances needs_clarification -> resolution_proposed, so
	// the record must be at needs_clarification for the proposal to be legal.
	seedContradiction(t, root, "c1", protocol.ContradictionStatusNeedsClarification)
	reader := &fakeContradictionReader{root: root}

	// Propose a resolution (needs_clarification -> resolution_proposed).
	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/contradictions/c1/propose-resolution",
		`{"proposed_statement":"use 3","rationale":"prod","requires_human_confirmation":false}`,
		map[string]string{"projectId": "proj-a", "id": "c1"}, ContradictionProposeResolutionHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("propose status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, http.MethodPost, "/api/v1/projects/proj-a/contradictions/c1/resolve",
		`{"statement":"use 3 retries","by":"semar"}`,
		map[string]string{"projectId": "proj-a", "id": "c1"}, ContradictionResolveHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Contradiction protocol.Contradiction `json:"contradiction"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Contradiction.Status != protocol.ContradictionStatusResolved {
		t.Fatalf("status = %q, want resolved", out.Contradiction.Status)
	}
}

func TestContradictionResolveIllegalTransition409(t *testing.T) {
	root := t.TempDir()
	// Resolving directly from detected skips the required proposal step.
	seedContradiction(t, root, "c1", protocol.ContradictionStatusDetected)
	reader := &fakeContradictionReader{root: root}

	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/contradictions/c1/resolve",
		`{"statement":"x","by":"semar"}`,
		map[string]string{"projectId": "proj-a", "id": "c1"}, ContradictionResolveHandler(reader))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "illegal_transition" {
		t.Fatalf("code = %q, want illegal_transition", body["code"])
	}
}
