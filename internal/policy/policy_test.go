package policy

import "testing"

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
