package policy

import (
	"os"
	"path/filepath"
	"testing"
)

const fixturePolicyPath = "../../test/fixtures/policy.yaml"

func TestLoadFromFixture(t *testing.T) {
	p, err := Load(fixturePolicyPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if p.Capabilities.Git.Commit != LevelAllow {
		t.Errorf("git.commit: got %q, want %q", p.Capabilities.Git.Commit, LevelAllow)
	}
	if p.Capabilities.Git.Push != LevelRequireApproval {
		t.Errorf("git.push: got %q, want %q", p.Capabilities.Git.Push, LevelRequireApproval)
	}
	if p.Capabilities.Git.ForcePush != LevelDeny {
		t.Errorf("git.force_push: got %q, want %q", p.Capabilities.Git.ForcePush, LevelDeny)
	}
	if p.Capabilities.Git.DefaultBranchWrite != LevelDeny {
		t.Errorf("git.default_branch_write: got %q, want %q", p.Capabilities.Git.DefaultBranchWrite, LevelDeny)
	}
	if p.Capabilities.Execution.TimeoutSeconds != 600 {
		t.Errorf("execution.timeout_seconds: got %d, want 600", p.Capabilities.Execution.TimeoutSeconds)
	}
	if p.Approvals.Scope != "run" {
		t.Errorf("approvals.scope: got %q, want the Default() value \"run\" to survive a fixture that doesn't override it", p.Approvals.Scope)
	}
}

func TestLoadApprovalsScopeOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte("approvals:\n  scope: day\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Approvals.Scope != "day" {
		t.Errorf("approvals.scope: got %q, want day", p.Approvals.Scope)
	}
	// Everything else must still fall back to Default(), not zero out.
	if p.Capabilities.Git.ForcePush != LevelDeny {
		t.Errorf("git.force_push: got %q, want the Default() value to survive an unrelated override", p.Capabilities.Git.ForcePush)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	p, err := Load("/nonexistent/policy.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Capabilities.Git.DefaultBranchWrite != LevelDeny {
		t.Fatalf("expected default policy to deny default-branch writes")
	}
}

func TestAllowsFilesystemWrite(t *testing.T) {
	p, err := Load(fixturePolicyPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		path string
		want bool
	}{
		{"workspace/src/main.go", true},
		{"workspace/.env", false},
		{"workspace/.ssh/id_rsa", false},
		{"workspace/secrets/token.txt", false},
		{"outside/main.go", false},
	}
	for _, c := range cases {
		got, err := p.AllowsFilesystemWrite(c.path)
		if err != nil {
			t.Fatalf("AllowsFilesystemWrite(%q): %v", c.path, err)
		}
		if got != c.want {
			t.Errorf("AllowsFilesystemWrite(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
