package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newSmokeWorkspace(t *testing.T) string {
	t.Helper()
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo-a")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "init", "-q", "-b", "main")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "add", "f.txt")
	runGit(t, repoDir, "commit", "-q", "-m", "init")

	pdir := filepath.Join(dir, ".punakawan")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "version: punakawan.workspace/v1\nid: smoke\nname: Smoke\nrepositories:\n  - id: repo-a\n    path: ./repo-a\n"
	if err := os.WriteFile(filepath.Join(pdir, "workspace.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runCLI(t *testing.T, workspaceDir string, args ...string) (string, error) {
	t.Helper()
	prevDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workspaceDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(prevDir) }()

	var buf bytes.Buffer
	root := newRootCmd()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err = root.Execute()
	return buf.String(), err
}

func TestWorkspaceShow(t *testing.T) {
	dir := newSmokeWorkspace(t)
	out, err := runCLI(t, dir, "workspace", "show")
	if err != nil {
		t.Fatalf("workspace show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "smoke") || !strings.Contains(out, "repo-a") {
		t.Fatalf("unexpected output: %s", out)
	}
}

// TestSetupWithNoCredentialsAvailableFailsActionablyWithoutHanging
// confirms `setup` never opens a subshell or blocks waiting on
// interactive input it cannot get in a non-interactive context (a script,
// CI, an agent's own shell-out): with no credential in the environment,
// the durable global credential file, or a real terminal on stdin, it
// must return promptly with an error naming exactly which values are
// still missing and where to put them, rather than pretending an
// ephemeral exported value would have been durable.
func TestSetupWithNoCredentialsAvailableFailsActionablyWithoutHanging(t *testing.T) {
	dir := newSmokeWorkspace(t)
	for _, name := range []string{"ATLASSIAN_HOST", "ATLASSIAN_API_TOKEN", "ATLASSIAN_EMAIL", "ATLASSIAN_API_TOKEN_SCOPED", "GITHUB_TOKEN", "GH_TOKEN"} {
		t.Setenv(name, "")
	}

	out, err := runCLI(t, dir, "setup")
	if err == nil {
		t.Fatalf("expected setup to fail with no credentials available, got output:\n%s", out)
	}
	for _, want := range []string{"atlassian", "github"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("setup error missing %q: %v", want, err)
		}
	}
}
