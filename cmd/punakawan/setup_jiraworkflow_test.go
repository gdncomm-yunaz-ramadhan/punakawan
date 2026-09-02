package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/jiraworkflow"
)

// newWorkspaceDir makes a directory that discovers as a real workspace and
// makes it the process's working directory for the duration of the test.
func newWorkspaceDir(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	root := t.TempDir()
	dir := filepath.Join(root, ".punakawan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .punakawan: %v", err)
	}
	repo := filepath.Join(root, "repo-a")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir repo-a: %v", err)
	}
	yaml := "version: punakawan.workspace/v1\nid: jira-workflow-setup\nname: Jira Workflow Setup\nrepositories:\n  - id: repo-a\n    path: ./repo-a\n"
	if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}

	prior, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prior) })
	return root
}

// TestJiraWorkflowSetupWritesAUsableConfigOnce covers both halves: a
// workspace with no config gets one that actually parses and has Jira
// write-back switched on, and a workspace that has already tuned its own
// config never loses it to a re-run.
func TestJiraWorkflowSetupWritesAUsableConfigOnce(t *testing.T) {
	root := newWorkspaceDir(t)
	path := filepath.Join(root, ".punakawan", "jira-workflow.yaml")

	cmd := &cobra.Command{}
	cmd.SetErr(&bytes.Buffer{})
	reportJiraWorkflowSetup(cmd)

	cfg, err := jiraworkflow.Load(path)
	if err != nil {
		t.Fatalf("Load the generated config: %v", err)
	}
	if !cfg.AutoLog {
		t.Fatal("AutoLog is false in the generated config; every automatic Jira update would stay off")
	}
	if !cfg.ShouldComment("delivery.started") || !cfg.ShouldComment("delivery.completed") {
		t.Fatalf("CommentEvents = %v, want the delivery lifecycle events", cfg.CommentEvents)
	}
	if !cfg.LogWork {
		t.Fatal("LogWork is false; measured intervals would never reach Jira")
	}
	if cfg.TransitionOnComplete {
		t.Fatal("TransitionOnComplete is true without any configured status names, which can only fail")
	}

	tuned := []byte("auto_log: false\n")
	if err := os.WriteFile(path, tuned, 0o644); err != nil {
		t.Fatalf("write tuned config: %v", err)
	}
	reportJiraWorkflowSetup(cmd)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(tuned) {
		t.Fatalf("config was rewritten on the second run:\n%s", got)
	}
}
