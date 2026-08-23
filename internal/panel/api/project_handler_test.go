package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/project"
)

// fakeProjectReader is an in-memory contract.ProjectReader backed by a single
// project, enough to exercise the handlers' happy paths and error mapping.
type fakeProjectReader struct {
	summaries map[string]contract.ProjectSummary
	projects  map[string]*project.Project
	// primaryID stands in for the workspace this panel instance was loaded
	// for, which Deregister must refuse.
	primaryID string
}

func newFakeProjectReader() *fakeProjectReader {
	return &fakeProjectReader{
		summaries: map[string]contract.ProjectSummary{
			"proj-a": {ID: "proj-a", Name: "Project A", Description: "desc", Path: "/tmp/a", Availability: "available", RepositoryCount: 2, MetadataCount: 1},
		},
		projects: map[string]*project.Project{
			"proj-a": {ID: "proj-a", Name: "Project A", Description: "desc", Path: "/tmp/a", Revision: 1, Metadata: []project.MetadataEntry{
				{Key: "jira.project_key", Description: "Jira key", Value: "TRF"},
			}},
		},
	}
}

func (f *fakeProjectReader) Deregister(ctx context.Context, id string) error {
	if id == f.primaryID {
		return contract.ErrPrimaryProject
	}
	if _, ok := f.summaries[id]; !ok {
		return contract.ErrWorkspaceUnavailable
	}
	delete(f.summaries, id)
	delete(f.projects, id)
	return nil
}

func (f *fakeProjectReader) List(ctx context.Context) ([]contract.ProjectSummary, error) {
	out := make([]contract.ProjectSummary, 0, len(f.summaries))
	for _, s := range f.summaries {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeProjectReader) Summary(ctx context.Context, id string) (contract.ProjectSummary, error) {
	s, ok := f.summaries[id]
	if !ok {
		return contract.ProjectSummary{}, contract.ErrWorkspaceUnavailable
	}
	return s, nil
}

func (f *fakeProjectReader) Get(ctx context.Context, id string) (*project.Project, error) {
	p, ok := f.projects[id]
	if !ok {
		return nil, contract.ErrWorkspaceUnavailable
	}
	// Return a copy so handlers cannot mutate the fake's state.
	cp := *p
	cp.Metadata = append([]project.MetadataEntry(nil), p.Metadata...)
	return &cp, nil
}

func (f *fakeProjectReader) AddMetadata(ctx context.Context, id string, entry project.MetadataEntry, baseRevision int) (*project.Project, error) {
	p, ok := f.projects[id]
	if !ok {
		return nil, contract.ErrWorkspaceUnavailable
	}
	cp := *p
	cp.Metadata = append([]project.MetadataEntry(nil), p.Metadata...)
	if err := cp.AddMetadata(entry, baseRevision); err != nil {
		return nil, err
	}
	f.projects[id] = &cp
	return &cp, nil
}

func (f *fakeProjectReader) UpdateMetadata(ctx context.Context, id, key string, newDescription *string, newValue any, baseRevision int) (*project.Project, error) {
	p, ok := f.projects[id]
	if !ok {
		return nil, contract.ErrWorkspaceUnavailable
	}
	cp := *p
	cp.Metadata = append([]project.MetadataEntry(nil), p.Metadata...)
	if err := cp.UpdateMetadata(key, newDescription, newValue, baseRevision); err != nil {
		return nil, err
	}
	f.projects[id] = &cp
	return &cp, nil
}

func (f *fakeProjectReader) DeleteMetadata(ctx context.Context, id, key string, baseRevision int) (*project.Project, error) {
	p, ok := f.projects[id]
	if !ok {
		return nil, contract.ErrWorkspaceUnavailable
	}
	cp := *p
	cp.Metadata = append([]project.MetadataEntry(nil), p.Metadata...)
	if err := cp.DeleteMetadata(key, baseRevision); err != nil {
		return nil, err
	}
	f.projects[id] = &cp
	return &cp, nil
}

func TestProjectsHandlerListsItems(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	ProjectsHandler(newFakeProjectReader())(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items []contract.ProjectSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != "proj-a" {
		t.Fatalf("items = %+v, want proj-a", body.Items)
	}
}

func TestProjectHandlerReturnsDetail(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/proj-a", nil)
	req.SetPathValue("projectId", "proj-a")
	rec := httptest.NewRecorder()
	ProjectHandler(newFakeProjectReader())(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var detail ProjectDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.ID != "proj-a" || detail.Revision != 1 || len(detail.Metadata) != 1 {
		t.Fatalf("detail = %+v, want proj-a rev 1 with 1 metadata entry", detail)
	}
	if detail.RepositoryCount != 2 {
		t.Fatalf("RepositoryCount = %d, want 2 (from summary)", detail.RepositoryCount)
	}
}

func doProjectDelete(t *testing.T, projectID string, reader contract.ProjectReader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+projectID, nil)
	req.SetPathValue("projectId", projectID)
	rec := httptest.NewRecorder()
	ProjectDeleteHandler(reader)(rec, req)
	return rec
}

func TestProjectDeleteHandlerDeregisters(t *testing.T) {
	reader := newFakeProjectReader()
	rec := doProjectDelete(t, "proj-a", reader)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
	if _, ok := reader.summaries["proj-a"]; ok {
		t.Fatalf("proj-a still registered after delete")
	}
}

func TestProjectDeleteHandlerUnknownReturns404(t *testing.T) {
	rec := doProjectDelete(t, "nope", newFakeProjectReader())

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestProjectDeleteHandlerRefusesPrimary(t *testing.T) {
	reader := newFakeProjectReader()
	reader.primaryID = "proj-a"
	rec := doProjectDelete(t, "proj-a", reader)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if _, ok := reader.summaries["proj-a"]; !ok {
		t.Fatalf("primary project was removed despite the 409")
	}
}

func TestProjectHandlerUnknownReturns404(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/nope", nil)
	req.SetPathValue("projectId", "nope")
	rec := httptest.NewRecorder()
	ProjectHandler(newFakeProjectReader())(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
