package deliverysummary

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/testrun"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestGatherReadsLedgerAndTestReport(t *testing.T) {
	workspaceRoot := t.TempDir()
	runID := "run-1"

	bundle, err := evidence.NewBundle(workspaceRoot, runID, "task-1")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	report := testrun.Report{
		AllPassed: false,
		Results: []testrun.CommandResult{
			{Command: testrun.Command{Name: "go", Args: []string{"build", "./..."}}, ExitCode: 0},
			{Command: testrun.Command{Name: "go", Args: []string{"test", "./..."}}, ExitCode: 1},
		},
	}
	if err := testrun.WriteBundle(report, bundle); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	ledger, err := evidence.OpenLedger(workspaceRoot, runID)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if _, err := evidence.RecordArtifact(ledger, runID, "task-1", protocol.EvidenceRecordTypeTestReport, bundle, "tests.json", time.Now().UTC()); err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}
	if err := ledger.Append(protocol.EvidenceRecord{Id: "ev-2", RunId: runID, Type: protocol.EvidenceRecordTypeGitDiff, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("ledger.Append: %v", err)
	}

	in, err := Gather(context.Background(), GatherInput{WorkspaceRoot: workspaceRoot, RunId: runID})
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(in.Evidence) != 2 {
		t.Fatalf("Evidence = %+v, want 2 records", in.Evidence)
	}
	if len(in.TestReports) != 1 || len(in.TestReports[0].Results) != 2 {
		t.Fatalf("TestReports = %+v, want one decoded report with 2 results", in.TestReports)
	}

	summary := Build(in)
	if summary.CommandsRun != 2 || summary.CommandsPassed != 1 || summary.CommandsFailed != 1 {
		t.Fatalf("summary = %+v, want 2 commands, 1 passed, 1 failed", summary)
	}
	if summary.EvidenceCount != 2 {
		t.Fatalf("EvidenceCount = %d, want 2", summary.EvidenceCount)
	}
}

func TestGatherPopulatesCommitsFromRange(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "base commit")
	runGit(t, dir, "branch", "base")

	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-m", "feature commit")

	sup := tools.New(dir)
	insp := gitops.NewInspector(sup)

	workspaceRoot := t.TempDir()
	in, err := Gather(context.Background(), GatherInput{
		WorkspaceRoot: workspaceRoot,
		RunId:         "run-1",
		Inspector:     insp,
		RepoPath:      dir,
		BaseBranch:    "base",
		HeadBranch:    "feature",
	})
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(in.Commits) != 1 || in.Commits[0].Subject != "feature commit" {
		t.Fatalf("Commits = %+v, want exactly the one commit on feature not on base", in.Commits)
	}
}

func TestURLsFromRefsFindsPrAndJiraLinks(t *testing.T) {
	prURL, jiraURL := URLsFromRefs([]string{
		"https://github.com/acme/widgets/pull/43",
		"https://acme.atlassian.net/browse/PAY-1",
		"not-a-url",
	})
	if prURL != "https://github.com/acme/widgets/pull/43" {
		t.Errorf("prURL = %q, want the pull request URL", prURL)
	}
	if jiraURL != "https://acme.atlassian.net/browse/PAY-1" {
		t.Errorf("jiraURL = %q, want the Jira browse URL", jiraURL)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
