package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newSmokeWorkspace creates a real workspace.yaml plus a real, clean git
// repository at <dir>/repo-a and returns dir.
func newSmokeWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	repoDir := filepath.Join(dir, "repo-a")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo-a: %v", err)
	}
	runGit(t, repoDir, "init", "-q", "-b", "main")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}
	runGit(t, repoDir, "add", "f.txt")
	runGit(t, repoDir, "commit", "-q", "-m", "init")

	punakawanDir := filepath.Join(dir, ".punakawan")
	if err := os.MkdirAll(punakawanDir, 0o755); err != nil {
		t.Fatalf("mkdir .punakawan: %v", err)
	}
	workspaceYAML := "version: punakawan.workspace/v1\nid: smoke\nname: Smoke\nrepositories:\n  - id: repo-a\n    path: ./repo-a\n"
	if err := os.WriteFile(filepath.Join(punakawanDir, "workspace.yaml"), []byte(workspaceYAML), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
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

// runCLI executes the CLI in-process (as if invoked as `punakawan <args...>`)
// with the working directory set to workspaceDir, and returns combined
// stdout/stderr.
func runCLI(t *testing.T, workspaceDir string, args ...string) (string, error) {
	t.Helper()

	prevDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(workspaceDir); err != nil {
		t.Fatalf("Chdir: %v", err)
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

func TestGitStatus(t *testing.T) {
	dir := newSmokeWorkspace(t)

	out, err := runCLI(t, dir, "git", "status", "repo-a")
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "branch: main") || !strings.Contains(out, "clean: true") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestDoctor(t *testing.T) {
	dir := newSmokeWorkspace(t)

	out, err := runCLI(t, dir, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	if !strings.Contains(out, "git") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestWorktreeRequestApproveCreateRemove(t *testing.T) {
	dir := newSmokeWorkspace(t)

	out, err := runCLI(t, dir, "worktree", "request", "repo-a", "task-1")
	if err != nil {
		t.Fatalf("worktree request: %v\n%s", err, out)
	}
	if !strings.Contains(out, "pending") {
		t.Fatalf("expected pending status, got: %s", out)
	}

	if _, err := runCLI(t, dir, "worktree", "approve", "repo-a", "task-1"); err != nil {
		t.Fatalf("worktree approve: %v", err)
	}

	out, err = runCLI(t, dir, "worktree", "create", "repo-a", "task-1")
	if err != nil {
		t.Fatalf("worktree create: %v\n%s", err, out)
	}
	wantBranch := filepath.Join(dir, ".punakawan", "worktrees", "repo-a", "task-1")
	if !strings.Contains(out, wantBranch) {
		t.Fatalf("expected worktree path %q in output: %s", wantBranch, out)
	}
	if _, err := os.Stat(wantBranch); err != nil {
		t.Fatalf("expected worktree directory to exist: %v", err)
	}

	if _, err := runCLI(t, dir, "worktree", "remove", "repo-a", "task-1"); err != nil {
		t.Fatalf("worktree remove: %v", err)
	}
	if _, err := os.Stat(wantBranch); !os.IsNotExist(err) {
		t.Fatalf("expected worktree directory to be removed, stat err = %v", err)
	}
}

func TestWorktreeCreateWithoutApprovalFailsFromCLI(t *testing.T) {
	dir := newSmokeWorkspace(t)

	if _, err := runCLI(t, dir, "worktree", "create", "repo-a", "task-2"); err == nil {
		t.Fatal("expected worktree create to fail without a prior approval")
	}
}

// TestApprovalsListApproveDeny exercises the generic approvals CLI against
// an adapter-operation-shaped approval record, which "worktree
// approve/deny" cannot resolve (it only knows the repo-id/task-id worktree
// shape) - this is the surface a human uses to grant a pending Jira write.
func TestApprovalsListApproveDeny(t *testing.T) {
	dir := newSmokeWorkspace(t)

	a, err := app.Load(dir)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	defer a.Close()

	rec := protocol.ApprovalRecord{
		Id:          "approval-adapter-atlassian-atlassian.addJiraComment-run-1",
		RunId:       "run-1",
		Operation:   protocol.ApprovalRecordOperationExternalWrite,
		RequestedBy: protocol.ApprovalRecordRequestedBySemar,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := a.Approvals.Append(rec); err != nil {
		t.Fatalf("seed pending approval: %v", err)
	}

	out, err := runCLI(t, dir, "approvals", "list")
	if err != nil {
		t.Fatalf("approvals list: %v\n%s", err, out)
	}
	if !strings.Contains(out, rec.Id) {
		t.Fatalf("expected pending approval id in output: %s", out)
	}

	if _, err := runCLI(t, dir, "approvals", "approve", rec.Id, "--by", "ygrip"); err != nil {
		t.Fatalf("approvals approve: %v", err)
	}

	out, err = runCLI(t, dir, "approvals", "list")
	if err != nil {
		t.Fatalf("approvals list after approve: %v\n%s", err, out)
	}
	if strings.Contains(out, rec.Id) {
		t.Fatalf("expected no pending approvals after approve, got: %s", out)
	}

	// Deny requires a fresh pending record; the first is already resolved.
	rec2 := rec
	rec2.Id = "approval-adapter-atlassian-atlassian.transitionJiraIssue-run-1"
	if err := a.Approvals.Append(rec2); err != nil {
		t.Fatalf("seed second pending approval: %v", err)
	}
	if _, err := runCLI(t, dir, "approvals", "deny", rec2.Id, "--by", "ygrip"); err != nil {
		t.Fatalf("approvals deny: %v", err)
	}
}
