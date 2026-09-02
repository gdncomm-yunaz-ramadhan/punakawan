package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRoleListReturnsFourRoles(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var out RoleListOutput
	callTool(t, cs, "role_list", nil, &out)

	if got, want := len(out.Roles), 4; got != want {
		t.Fatalf("len(Roles) = %d, want %d", got, want)
	}
	wantIDs := map[string]bool{"semar": true, "gareng": true, "petruk": true, "bagong": true}
	for _, r := range out.Roles {
		if !wantIDs[r.ID] {
			t.Errorf("unexpected role id %q", r.ID)
		}
		if r.Name == "" || r.Version == "" {
			t.Errorf("role %q: Name/Version should not be empty: %+v", r.ID, r)
		}
		delete(wantIDs, r.ID)
	}
	if len(wantIDs) > 0 {
		t.Errorf("missing role ids: %v", wantIDs)
	}
}

func TestRoleGetBagongMatchesManifest(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var out RoleGetOutput
	callTool(t, cs, "role_get", map[string]any{"id": "bagong"}, &out)

	if out.Role.ID != "bagong" {
		t.Fatalf("Role.ID = %q, want %q", out.Role.ID, "bagong")
	}
	if out.Role.OutputSchemaID != "bagong_review" {
		t.Errorf("Role.OutputSchemaID = %q, want %q", out.Role.OutputSchemaID, "bagong_review")
	}
	if !out.Role.ToolPolicy.ReadOnly {
		t.Errorf("Role.ToolPolicy.ReadOnly = false, want true")
	}
	if !out.Role.ExecutionPolicy.RequiresEvidence || out.Role.ExecutionPolicy.CanMutate {
		t.Errorf("Role.ExecutionPolicy = %+v, want RequiresEvidence=true, CanMutate=false", out.Role.ExecutionPolicy)
	}
	if len(out.Role.Instructions) < 100 {
		t.Errorf("Role.Instructions looks unresolved (len=%d), want the full prompt.md text, got: %q", len(out.Role.Instructions), out.Role.Instructions)
	}
}

func TestRoleGetUnknownIDErrors(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	cs := connect(t, a)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "role_get", Arguments: map[string]any{"id": "not-a-role"}})
	if err != nil {
		t.Fatalf("CallTool(role_get): %v", err)
	}
	if !res.IsError {
		t.Fatalf("role_get(not-a-role) = %+v, want an error result", res)
	}
}
