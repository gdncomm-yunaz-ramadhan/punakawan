package mcpserver

import "testing"

// TestCreateWorkflowRunStampsRoleConfig verifies ROLE-012: a newly created run
// carries the role-config revision and an effective-role settings snapshot for
// all four roles, so the run stays reproducible after later config edits.
func TestCreateWorkflowRunStampsRoleConfig(t *testing.T) {
	a := newTestApp(t)
	newTestRun(t, a, "run-1")

	run, err := a.Workflow.Get("run-1")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}

	if run.RoleConfigRevision == nil {
		t.Fatal("expected run.RoleConfigRevision to be stamped")
	}
	if *run.RoleConfigRevision != 0 {
		t.Errorf("role_config_revision = %d, want 0 (defaults, no roles.yaml)", *run.RoleConfigRevision)
	}

	if run.EffectiveRoleSettings == nil {
		t.Fatal("expected run.EffectiveRoleSettings snapshot")
	}
	for _, role := range []string{"semar", "gareng", "petruk", "bagong"} {
		if _, ok := run.EffectiveRoleSettings[role]; !ok {
			t.Errorf("effective_role_settings missing role %q", role)
		}
	}
	// Under §7 defaults petruk executes.
	if got := run.EffectiveRoleSettings["petruk"]["mode"]; got != "execute" {
		t.Errorf("petruk mode = %v, want execute", got)
	}
}
