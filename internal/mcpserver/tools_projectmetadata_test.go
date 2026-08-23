package mcpserver

import "testing"

// This test exercises punokawan-999 through the real MCP tool dispatch
// (connect + callTool), not just selectProjectMetadata in isolation: an agent
// calling build_context_dossier must get the project's relevant metadata
// folded into the returned dossier as references.

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
