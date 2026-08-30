package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeRolesReader is an in-memory contract.RolesReader backed by a single
// project's preferences, seeded with roleconfig.Defaults(). It reuses the
// real roleconfig.Update/Reset so the handlers' error mapping is exercised
// against the same errors the production ProjectSource returns.
type fakeRolesReader struct {
	cfgs map[string]*protocol.RolePreferences
}

func newFakeRolesReader() *fakeRolesReader {
	d := roleconfig.Defaults()
	return &fakeRolesReader{
		cfgs: map[string]*protocol.RolePreferences{"proj-a": &d},
	}
}

func (f *fakeRolesReader) GetRoles(ctx context.Context, projectID string) (*protocol.RolePreferences, error) {
	cfg, ok := f.cfgs[projectID]
	if !ok {
		return nil, contract.ErrWorkspaceUnavailable
	}
	cp := *cfg
	return &cp, nil
}

func (f *fakeRolesReader) UpdateRole(ctx context.Context, projectID, role string, patch roleconfig.Patch, baseRevision int) (*protocol.RolePreferences, error) {
	cfg, ok := f.cfgs[projectID]
	if !ok {
		return nil, contract.ErrWorkspaceUnavailable
	}
	cp := *cfg
	if err := roleconfig.Update(&cp, roleconfig.Role(role), patch, baseRevision); err != nil {
		return nil, err
	}
	f.cfgs[projectID] = &cp
	return &cp, nil
}

func (f *fakeRolesReader) ResetRole(ctx context.Context, projectID, role string, baseRevision int) (*protocol.RolePreferences, error) {
	cfg, ok := f.cfgs[projectID]
	if !ok {
		return nil, contract.ErrWorkspaceUnavailable
	}
	cp := *cfg
	if err := roleconfig.Reset(&cp, roleconfig.Role(role), baseRevision); err != nil {
		return nil, err
	}
	f.cfgs[projectID] = &cp
	return &cp, nil
}

func doRoles(t *testing.T, method, target, projectID, role, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.SetPathValue("projectId", projectID)
	if role != "" {
		r.SetPathValue("role", role)
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

func TestRolesListReturnsRolesAndRevision(t *testing.T) {
	rec := doRoles(t, http.MethodGet, "/api/v1/projects/proj-a/roles", "proj-a", "", "",
		RolesListHandler(newFakeRolesReader()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Roles    protocol.RolePreferencesRoles `json:"roles"`
		Revision int                           `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Revision != 0 {
		t.Fatalf("revision = %d, want 0 (defaults)", body.Revision)
	}
	if body.Roles.Semar.Style != protocol.RolePreferenceStyleBalanced {
		t.Fatalf("semar style = %q, want balanced (defaults)", body.Roles.Semar.Style)
	}
}

func TestRoleUpdateHappyPathBumpsRevision(t *testing.T) {
	rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/semar", "proj-a", "semar",
		`{"style":"strict","instructions":"Prefer reversible migrations.","base_revision":0}`,
		RoleUpdateHandler(newFakeRolesReader()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Roles    protocol.RolePreferencesRoles `json:"roles"`
		Revision int                           `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Revision != 1 {
		t.Fatalf("revision = %d, want 1", body.Revision)
	}
	if body.Roles.Semar.Style != protocol.RolePreferenceStyleStrict {
		t.Fatalf("semar style = %q, want strict after patch", body.Roles.Semar.Style)
	}
	if body.Roles.Semar.Instructions != "Prefer reversible migrations." {
		t.Fatalf("semar instructions = %q, unexpected", body.Roles.Semar.Instructions)
	}
}

func TestRoleUpdateRevisionConflict(t *testing.T) {
	rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/semar", "proj-a", "semar",
		`{"style":"strict","base_revision":99}`,
		RoleUpdateHandler(newFakeRolesReader()))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "revision_conflict" {
		t.Fatalf("code = %v, want revision_conflict", body["code"])
	}
	// The current revision is reported so the client can rebase.
	if body["current_revision"] != float64(0) {
		t.Fatalf("current_revision = %v, want 0", body["current_revision"])
	}
}

func TestRoleUpdateInvalidStyle(t *testing.T) {
	rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/semar", "proj-a", "semar",
		`{"style":"chaotic","base_revision":0}`,
		RoleUpdateHandler(newFakeRolesReader()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "invalid_style" {
		t.Fatalf("code = %q, want invalid_style", body["code"])
	}
}

func TestRoleUpdateInstructionsTooLong(t *testing.T) {
	tooLong := strings.Repeat("a", 2001)
	rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/semar", "proj-a", "semar",
		`{"instructions":"`+tooLong+`","base_revision":0}`,
		RoleUpdateHandler(newFakeRolesReader()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "instructions_too_long" {
		t.Fatalf("code = %q, want instructions_too_long", body["code"])
	}
}

func TestRoleUpdateUnknownRole404(t *testing.T) {
	rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/nobody", "proj-a", "nobody",
		`{"style":"strict","base_revision":0}`,
		RoleUpdateHandler(newFakeRolesReader()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "unknown_role" {
		t.Fatalf("code = %q, want unknown_role", body["code"])
	}
}

func TestRoleResetRestoresDefaults(t *testing.T) {
	reader := newFakeRolesReader()
	// First change semar away from defaults (rev 0 -> 1), then reset it back (rev 1 -> 2).
	if rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/semar", "proj-a", "semar",
		`{"style":"strict","base_revision":0}`, RoleUpdateHandler(reader)); rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	rec := doRoles(t, http.MethodPost, "/api/v1/projects/proj-a/roles/semar/reset", "proj-a", "semar",
		`{"base_revision":1}`, RoleResetHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Roles    protocol.RolePreferencesRoles `json:"roles"`
		Revision int                           `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Revision != 2 {
		t.Fatalf("revision = %d, want 2", body.Revision)
	}
	if body.Roles.Semar.Style != protocol.RolePreferenceStyleBalanced {
		t.Fatalf("semar style = %q, want balanced after reset to defaults", body.Roles.Semar.Style)
	}
}
