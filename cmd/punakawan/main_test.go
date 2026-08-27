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

func TestSetupPrintsSourceableScripts(t *testing.T) {
	dir := newSmokeWorkspace(t)

	sh, err := runCLI(t, dir, "setup", "--shell", "sh")
	if err != nil {
		t.Fatalf("setup sh: %v\n%s", err, sh)
	}
	for _, want := range []string{"ATLASSIAN_HOST", "ATLASSIAN_API_TOKEN", "ATLASSIAN_EMAIL", "GITHUB_TOKEN", "GH_TOKEN", "stty -echo"} {
		if !strings.Contains(sh, want) {
			t.Errorf("setup sh missing %q:\n%s", want, sh)
		}
	}

	powershell, err := runCLI(t, dir, "setup", "--shell", "powershell")
	if err != nil {
		t.Fatalf("setup powershell: %v\n%s", err, powershell)
	}
	if !strings.Contains(powershell, "SecureStringToBSTR") {
		t.Fatalf("setup powershell does not hide token input:\n%s", powershell)
	}

	_, err = runCLI(t, dir, "setup", "--shell", "fish")
	if err == nil || !strings.Contains(err.Error(), "unsupported setup shell") {
		t.Fatalf("setup fish error = %v, want unsupported shell", err)
	}
}
