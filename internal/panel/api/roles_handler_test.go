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
// project's configuration, seeded with roleconfig.Defaults(). It reuses the
// real roleconfig.Update/Reset so the handlers' error mapping is exercised
// against the same errors the production ProjectSource returns.
type fakeRolesReader struct {
	cfgs map[string]*protocol.RoleConfiguration
}

func newFakeRolesReader() *fakeRolesReader {
	d := roleconfig.Defaults()
	return &fakeRolesReader{
		cfgs: map[string]*protocol.RoleConfiguration{"proj-a": &d},
	}
}

func (f *fakeRolesReader) GetRoles(ctx context.Context, projectID string) (*protocol.RoleConfiguration, []contract.RoleCapabilityInfo, error) {
	cfg, ok := f.cfgs[projectID]
	if !ok {
		return nil, nil, contract.ErrWorkspaceUnavailable
	}
	owned := make([]contract.RoleCapabilityInfo, 0, len(roleconfig.AllRoles))
	for _, role := range roleconfig.AllRoles {
		owned = append(owned, contract.RoleCapabilityInfo{
			Role:         string(role),
			Capabilities: roleconfig.OwnedCapabilities(role),
		})
	}
	cp := *cfg
	return &cp, owned, nil
}

func (f *fakeRolesReader) UpdateRole(ctx context.Context, projectID, role string, patch roleconfig.Patch, baseRevision int) (*protocol.RoleConfiguration, error) {
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

func (f *fakeRolesReader) ResetRole(ctx context.Context, projectID, role string, baseRevision int) (*protocol.RoleConfiguration, error) {
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

func TestRolesListReturnsRolesRevisionAndOwned(t *testing.T) {
	rec := doRoles(t, http.MethodGet, "/api/v1/projects/proj-a/roles", "proj-a", "", "",
		RolesListHandler(newFakeRolesReader()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Roles    protocol.RoleConfigurationRoles `json:"roles"`
		Revision int                             `json:"revision"`
		Owned    []contract.RoleCapabilityInfo   `json:"owned"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Revision != 0 {
		t.Fatalf("revision = %d, want 0 (defaults)", body.Revision)
	}
	if !body.Roles.Semar.Enabled {
		t.Fatalf("semar enabled = false, want true (defaults)")
	}
	if len(body.Owned) != 4 {
		t.Fatalf("owned = %+v, want 4 role catalogs", body.Owned)
	}
}

func TestRoleUpdateHappyPathBumpsRevision(t *testing.T) {
	rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/semar", "proj-a", "semar",
		`{"enabled":false,"base_revision":0}`,
		RoleUpdateHandler(newFakeRolesReader()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Roles    protocol.RoleConfigurationRoles `json:"roles"`
		Revision int                             `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Revision != 1 {
		t.Fatalf("revision = %d, want 1", body.Revision)
	}
	if body.Roles.Semar.Enabled {
		t.Fatalf("semar enabled = true, want false after patch")
	}
}

func TestRoleUpdateRevisionConflict(t *testing.T) {
	rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/semar", "proj-a", "semar",
		`{"enabled":false,"base_revision":99}`,
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

func TestRoleUpdateUnownedCapability(t *testing.T) {
	// modify_files is Petruk's, not Semar's, so patching it onto semar is rejected.
	rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/semar", "proj-a", "semar",
		`{"capabilities":{"modify_files":true},"base_revision":0}`,
		RoleUpdateHandler(newFakeRolesReader()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "unowned_capability" {
		t.Fatalf("code = %q, want unowned_capability", body["code"])
	}
}

func TestRoleUpdateUnknownRole404(t *testing.T) {
	rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/nobody", "proj-a", "nobody",
		`{"enabled":false,"base_revision":0}`,
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
	// First disable semar (rev 0 -> 1), then reset it back (rev 1 -> 2).
	if rec := doRoles(t, http.MethodPatch, "/api/v1/projects/proj-a/roles/semar", "proj-a", "semar",
		`{"enabled":false,"base_revision":0}`, RoleUpdateHandler(reader)); rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	rec := doRoles(t, http.MethodPost, "/api/v1/projects/proj-a/roles/semar/reset", "proj-a", "semar",
		`{"base_revision":1}`, RoleResetHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Roles    protocol.RoleConfigurationRoles `json:"roles"`
		Revision int                             `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Revision != 2 {
		t.Fatalf("revision = %d, want 2", body.Revision)
	}
	if !body.Roles.Semar.Enabled {
		t.Fatalf("semar enabled = false, want true after reset to defaults")
	}
}
