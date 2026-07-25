package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/roleconfig"
)

// TestRoleResolverAppliesWorkflowCapabilityRestriction verifies the shared §47
// resolver wired onto App applies a workflow definition's per-role restriction
// (ROLE-010): a workflow that turns off petruk.create_pull_request yields an
// Effective petruk without that capability, while capabilities the workflow did
// not touch stay on (reduce-never-increase).
func TestRoleResolverAppliesWorkflowCapabilityRestriction(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "repo-a"), 0o755); err != nil {
		t.Fatalf("mkdir repo-a: %v", err)
	}
	punakawanDir := filepath.Join(dir, ".punakawan")
	if err := os.MkdirAll(punakawanDir, 0o755); err != nil {
		t.Fatalf("mkdir .punakawan: %v", err)
	}
	workspaceYAML := "version: punakawan.workspace/v1\nid: smoke\nname: Smoke\nrepositories:\n  - id: repo-a\n    path: ./repo-a\n"
	if err := os.WriteFile(filepath.Join(punakawanDir, "workspace.yaml"), []byte(workspaceYAML), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}

	// A workflow definition that restricts petruk: create_pull_request off.
	workflowsDir := filepath.Join(punakawanDir, "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	defYAML := "version: punakawan.workflow/v1\n" +
		"id: restrict-petruk\n" +
		"name: Restrict Petruk\n" +
		"steps: []\n" +
		"roles:\n" +
		"  petruk:\n" +
		"    capabilities:\n" +
		"      create_pull_request: false\n" +
		"revision: 1\n"
	if err := os.WriteFile(filepath.Join(workflowsDir, "restrict-petruk.yaml"), []byte(defYAML), 0o644); err != nil {
		t.Fatalf("write workflow def: %v", err)
	}

	a, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	if a.RoleConfig == nil {
		t.Fatal("expected App.RoleConfig resolver to be wired")
	}

	// With no workflow the project defaults leave create_pull_request on.
	base, err := a.RoleConfig.Effective("", "", roleconfig.Petruk)
	if err != nil {
		t.Fatalf("Effective (no workflow): %v", err)
	}
	if !base.Capabilities["create_pull_request"] {
		t.Fatal("expected create_pull_request on by default (no workflow restriction)")
	}

	// Under the restricting workflow it must be reduced to off, while an
	// untouched capability (plans) stays on.
	eff, err := a.RoleConfig.Effective("", "restrict-petruk", roleconfig.Petruk)
	if err != nil {
		t.Fatalf("Effective (restrict-petruk): %v", err)
	}
	if eff.Capabilities["create_pull_request"] {
		t.Error("expected workflow to disable petruk.create_pull_request")
	}
	if !eff.Capabilities["plans"] {
		t.Error("expected untouched petruk.plans capability to remain on")
	}
}
