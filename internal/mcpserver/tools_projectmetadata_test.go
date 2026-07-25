package mcpserver

import "testing"

// These two tests exercise punokawan-999 through the real MCP tool dispatch
// (connect + callTool), not just selectProjectMetadata in isolation: an agent
// calling request_capsule or build_context_dossier must get the project's
// relevant metadata folded into the returned capsule/dossier as references.

func TestRequestCapsuleInjectsProjectMetadata(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	writeProjectMetadata(t, a.Workspace.Root)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "request_capsule", map[string]any{
		"task_id":    "bd-task-1",
		"role":       "petruk",
		"objective":  "implement the refund flow",
		"capability": "jira",
	}, &out)

	pm, _ := out["project_metadata"].([]any)
	if len(pm) == 0 {
		t.Fatalf("request_capsule project_metadata empty; want jira.* entries injected: %+v", out["project_metadata"])
	}
	first, _ := pm[0].(map[string]any)
	if first["key"] == nil || first["rendered"] == nil {
		t.Fatalf("project_metadata entry missing key/rendered: %+v", first)
	}
}

func TestBuildContextDossierInjectsProjectMetadata(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	writeProjectMetadata(t, a.Workspace.Root)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "build_context_dossier", map[string]any{
		"run_id":     "run-1",
		"capability": "jira",
		"user_goal":  "test the dossier metadata injection",
	}, &out)

	dossier, _ := out["dossier"].(map[string]any)
	if dossier == nil {
		t.Fatalf("build_context_dossier output has no dossier: %+v", out)
	}
	pm, _ := dossier["project_metadata"].([]any)
	if len(pm) == 0 {
		t.Fatalf("dossier project_metadata empty; want jira.* entries baked into the persisted dossier: %+v", dossier["project_metadata"])
	}
}
