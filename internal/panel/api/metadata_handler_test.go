package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/project"
)

func doMetadata(t *testing.T, method, target, projectID, key, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.SetPathValue("projectId", projectID)
	if key != "" {
		r.SetPathValue("key", key)
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

func TestMetadataListReturnsItemsAndRevision(t *testing.T) {
	rec := doMetadata(t, http.MethodGet, "/api/v1/projects/proj-a/metadata", "proj-a", "", "",
		MetadataListHandler(newFakeProjectReader()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Items    []project.MetadataEntry `json:"items"`
		Revision int                     `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Revision != 1 || len(body.Items) != 1 {
		t.Fatalf("body = %+v, want rev 1 with 1 item", body)
	}
}

func TestMetadataCreateHappyPath(t *testing.T) {
	rec := doMetadata(t, http.MethodPost, "/api/v1/projects/proj-a/metadata", "proj-a", "",
		`{"key":"team.owner","description":"Owner team","value":"AFF","base_revision":1}`,
		MetadataCreateHandler(newFakeProjectReader()))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Entry    project.MetadataEntry `json:"entry"`
		Revision int                   `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Entry.Key != "team.owner" || body.Revision != 2 {
		t.Fatalf("body = %+v, want team.owner rev 2", body)
	}
}

func TestMetadataCreateRevisionConflict(t *testing.T) {
	rec := doMetadata(t, http.MethodPost, "/api/v1/projects/proj-a/metadata", "proj-a", "",
		`{"key":"team.owner","description":"Owner","value":"AFF","base_revision":0}`,
		MetadataCreateHandler(newFakeProjectReader()))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "revision_conflict" {
		t.Fatalf("code = %v, want revision_conflict", body["code"])
	}
	// The current revision is reported so the client can rebase.
	if body["revision"] != float64(1) {
		t.Fatalf("revision = %v, want 1", body["revision"])
	}
}

func TestMetadataCreateDuplicateKey(t *testing.T) {
	rec := doMetadata(t, http.MethodPost, "/api/v1/projects/proj-a/metadata", "proj-a", "",
		`{"key":"jira.project_key","description":"dup","value":"X","base_revision":1}`,
		MetadataCreateHandler(newFakeProjectReader()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "duplicate_key" {
		t.Fatalf("code = %q, want duplicate_key", body["code"])
	}
}

func TestMetadataCreateSecretRejected(t *testing.T) {
	rec := doMetadata(t, http.MethodPost, "/api/v1/projects/proj-a/metadata", "proj-a", "",
		`{"key":"atlassian.api_token","description":"token","value":"x","base_revision":1}`,
		MetadataCreateHandler(newFakeProjectReader()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "secret_rejected" {
		t.Fatalf("code = %q, want secret_rejected", body["code"])
	}
}

func TestMetadataCreateMissingField(t *testing.T) {
	rec := doMetadata(t, http.MethodPost, "/api/v1/projects/proj-a/metadata", "proj-a", "",
		`{"key":"a.b","description":"","value":"x","base_revision":1}`,
		MetadataCreateHandler(newFakeProjectReader()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "missing_field" {
		t.Fatalf("code = %q, want missing_field", body["code"])
	}
}

func TestMetadataUpdateHappyPath(t *testing.T) {
	rec := doMetadata(t, http.MethodPatch, "/api/v1/projects/proj-a/metadata/jira.project_key", "proj-a", "jira.project_key",
		`{"value":"NEW","base_revision":1}`,
		MetadataUpdateHandler(newFakeProjectReader()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Entry    project.MetadataEntry `json:"entry"`
		Revision int                   `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Entry.Value != "NEW" || body.Revision != 2 {
		t.Fatalf("body = %+v, want value NEW rev 2", body)
	}
	// Description was not supplied, so it must be preserved.
	if body.Entry.Description != "Jira key" {
		t.Fatalf("description = %q, want preserved 'Jira key'", body.Entry.Description)
	}
}

func TestMetadataUpdateUnknownKey404(t *testing.T) {
	rec := doMetadata(t, http.MethodPatch, "/api/v1/projects/proj-a/metadata/no.such", "proj-a", "no.such",
		`{"value":"x","base_revision":1}`,
		MetadataUpdateHandler(newFakeProjectReader()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMetadataDeleteHappyPath(t *testing.T) {
	rec := doMetadata(t, http.MethodDelete, "/api/v1/projects/proj-a/metadata/jira.project_key?base_revision=1", "proj-a", "jira.project_key", "",
		MetadataDeleteHandler(newFakeProjectReader()))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestMetadataDeleteMissingBaseRevision(t *testing.T) {
	rec := doMetadata(t, http.MethodDelete, "/api/v1/projects/proj-a/metadata/jira.project_key", "proj-a", "jira.project_key", "",
		MetadataDeleteHandler(newFakeProjectReader()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "missing_field" {
		t.Fatalf("code = %q, want missing_field", body["code"])
	}
}

func TestMetadataDeleteConflict(t *testing.T) {
	rec := doMetadata(t, http.MethodDelete, "/api/v1/projects/proj-a/metadata/jira.project_key?base_revision=99", "proj-a", "jira.project_key", "",
		MetadataDeleteHandler(newFakeProjectReader()))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}
