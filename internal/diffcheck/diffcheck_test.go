package diffcheck

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/tools"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "first commit")
	return dir
}

func newBundle(t *testing.T) *evidence.Bundle {
	t.Helper()
	b, err := evidence.NewBundle(t.TempDir(), "run-1", "task-1")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	return b
}

func TestCheckAllowsCleanChange(t *testing.T) {
	repo := newRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	sup := tools.New(repo)
	bundle := newBundle(t)
	report, err := Check(context.Background(), sup, repo, policy.Default(), bundle)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.Allowed {
		t.Fatalf("expected report to be allowed, violations: %v", report.Violations)
	}
	if len(report.ChangedFiles) != 1 || report.ChangedFiles[0] != "main.go" {
		t.Fatalf("ChangedFiles: got %v", report.ChangedFiles)
	}

	if _, err := os.Stat(bundle.Path("diff.patch")); err != nil {
		t.Fatalf("expected diff.patch to be written: %v", err)
	}
}

func TestCheckBlocksPolicyDeniedFile(t *testing.T) {
	repo := newRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, "secrets"), 0o755); err != nil {
		t.Fatalf("mkdir secrets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "secrets", "token.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write secrets/token.txt: %v", err)
	}

	pol := &policy.Policy{Capabilities: policy.Capabilities{Filesystem: policy.FilesystemPolicy{
		Deny: []string{"secrets/**"},
	}}}

	sup := tools.New(repo)
	report, err := Check(context.Background(), sup, repo, pol, newBundle(t))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Allowed {
		t.Fatal("expected report to be disallowed for a policy-denied path")
	}
	if len(report.Violations) == 0 {
		t.Fatal("expected at least one violation")
	}
}

func TestCheckBlocksLikelySecret(t *testing.T) {
	repo := newRepo(t)
	content := "const key = \"AKIAABCDEFGHIJKLMNOP\"\n"
	if err := os.WriteFile(filepath.Join(repo, "config.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config.go: %v", err)
	}

	sup := tools.New(repo)
	report, err := Check(context.Background(), sup, repo, policy.Default(), newBundle(t))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Allowed {
		t.Fatal("expected report to be disallowed for an AWS-key-shaped secret")
	}
}

func TestCheckIncludesNewAndDeletedFiles(t *testing.T) {
	repo := newRepo(t)
	if err := os.Remove(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("remove README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write new.txt: %v", err)
	}

	sup := tools.New(repo)
	report, err := Check(context.Background(), sup, repo, policy.Default(), newBundle(t))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.Allowed {
		t.Fatalf("expected report to be allowed, violations: %v", report.Violations)
	}

	want := map[string]bool{"README.md": false, "new.txt": false}
	for _, f := range report.ChangedFiles {
		if _, ok := want[f]; ok {
			want[f] = true
		}
	}
	for f, seen := range want {
		if !seen {
			t.Errorf("expected %q in ChangedFiles, got %v", f, report.ChangedFiles)
		}
	}
}
