package mcpserver

import "testing"

// TestCapabilityRegistryMatchesRegistration is the anti-drift guard for
// agent-context plan §4.3. It asserts that CapabilityRegistry enumerates the
// tools the server actually registers — in particular the tools the old
// hand-maintained workflowdef.KnownMCPCapabilities mirror had dropped, which
// is precisely the drift this phase eliminates.
func TestCapabilityRegistryMatchesRegistration(t *testing.T) {
	a := newTestApp(t)
	reg := CapabilityRegistry(a)

	// A tool present in both the mirror and the registry (baseline).
	if !reg.Has("write_files") {
		t.Fatalf("registry missing a core registered tool: write_files")
	}

	// Tools that were registered but ABSENT from the old mirror. Before this
	// change a workflow step naming any of these failed validation with
	// ErrUnknownCapability even though the server exposed them.
	for _, name := range []string{
		"submit_contradiction",
		"analyze_impact",
		"record_impact_edge",
		"start_delivery",
		"jira_assign_issue",
		"list_pending_approvals",
	} {
		if !reg.Has(name) {
			t.Errorf("registry missing registered-but-unmirrored tool %q", name)
		}
	}

	// The registry should enumerate the full tool surface, not the mirror's
	// stale subset.
	if got := reg.Len(); got < 60 {
		t.Errorf("registry Len = %d, expected the full tool surface (~70)", got)
	}
}
