package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ygrip/punakawan/internal/handoff"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeHandoffReader delegates to the real internal/handoff store rooted at a
// temp dir. Validate/Resume run with empty ValidationDeps (every check treated
// as passing), which is enough to exercise the superseded-refusal path since
// supersession is checked before any dep.
type fakeHandoffReader struct{ root string }

func (f *fakeHandoffReader) ListHandoffs(ctx context.Context, projectID string) ([]protocol.HandoffCapsule, error) {
	ids, err := handoff.List(f.root)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.HandoffCapsule, 0, len(ids))
	for _, id := range ids {
		h, err := handoff.Get(f.root, id)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

func (f *fakeHandoffReader) GetHandoff(ctx context.Context, projectID, id string) (protocol.HandoffCapsule, error) {
	return handoff.Get(f.root, id)
}

func (f *fakeHandoffReader) CreateHandoff(ctx context.Context, projectID string, h protocol.HandoffCapsule) (protocol.HandoffCapsule, error) {
	return handoff.Create(f.root, h)
}

func (f *fakeHandoffReader) ValidateHandoff(ctx context.Context, projectID, id string) (handoff.ValidationResult, error) {
	return handoff.Validate(f.root, id, handoff.ValidationDeps{})
}

func (f *fakeHandoffReader) ResumeHandoff(ctx context.Context, projectID, id string) (map[string]any, error) {
	res, err := handoff.Validate(f.root, id, handoff.ValidationDeps{})
	if err != nil {
		return nil, err
	}
	if res.Status == handoff.StatusSuperseded {
		return nil, contract.ErrHandoffSuperseded
	}
	return handoff.ResumeContext(f.root, id)
}

func (f *fakeHandoffReader) SupersedeHandoff(ctx context.Context, projectID, id string) (protocol.HandoffCapsule, error) {
	if err := handoff.Supersede(f.root, id); err != nil {
		return protocol.HandoffCapsule{}, err
	}
	return handoff.Get(f.root, id)
}

// createHandoffViaHandler POSTs a new capsule and returns its assigned id.
func createHandoffViaHandler(t *testing.T, reader *fakeHandoffReader) string {
	t.Helper()
	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/handoffs",
		`{"objective":{"statement":"finish feature X"},"current_phase":"implementation","project_id":"proj","run_id":"run-1"}`,
		map[string]string{"projectId": "proj-a"}, HandoffCreateHandler(reader))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Handoff protocol.HandoffCapsule `json:"handoff"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if out.Handoff.Id == "" {
		t.Fatalf("server did not assign a handoff id")
	}
	return out.Handoff.Id
}

func TestHandoffCreateAndList(t *testing.T) {
	reader := &fakeHandoffReader{root: t.TempDir()}
	id := createHandoffViaHandler(t, reader)

	rec := doReq(t, http.MethodGet, "/api/v1/projects/proj-a/handoffs", "",
		map[string]string{"projectId": "proj-a"}, HandoffsListHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var list struct {
		Items []protocol.HandoffCapsule `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Id != id {
		t.Fatalf("items = %+v, want one %q", list.Items, id)
	}
}

func TestHandoffValidateResumable(t *testing.T) {
	reader := &fakeHandoffReader{root: t.TempDir()}
	id := createHandoffViaHandler(t, reader)

	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/handoffs/"+id+"/validate", "",
		map[string]string{"projectId": "proj-a", "id": id}, HandoffValidateHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("validate status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Status != string(handoff.StatusResumable) {
		t.Fatalf("status = %q, want resumable", out.Status)
	}
}

func TestHandoffResumeSuperseded409(t *testing.T) {
	reader := &fakeHandoffReader{root: t.TempDir()}
	id := createHandoffViaHandler(t, reader)

	// Supersede the capsule, then attempt to resume it.
	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/handoffs/"+id+"/supersede", "",
		map[string]string{"projectId": "proj-a", "id": id}, HandoffSupersedeHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("supersede status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, http.MethodPost, "/api/v1/projects/proj-a/handoffs/"+id+"/resume", "",
		map[string]string{"projectId": "proj-a", "id": id}, HandoffResumeHandler(reader))
	if rec.Code != http.StatusConflict {
		t.Fatalf("resume status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "superseded" {
		t.Fatalf("code = %q, want superseded", body["code"])
	}
}
