package handoffprobe

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInit initializes a git repo at dir, skipping the test if git is not on
// PATH (mirrors how the git-dependent tests elsewhere guard themselves so CI
// without git still passes).
func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available on PATH")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	cmd := exec.Command("git", "init", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v: %s", dir, err, out)
	}
}

func TestRepositoryStateMatches_GitInitRepoMatches(t *testing.T) {
	root := t.TempDir()
	gitInit(t, filepath.Join(root, "repo"))

	probe := RepositoryStateMatches(root)
	matches, err := probe([]string{"repo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matches {
		t.Fatalf("git init'd repo: got matches=false, want true")
	}
}

func TestRepositoryStateMatches_MissingRepoDoesNotMatch(t *testing.T) {
	root := t.TempDir()

	probe := RepositoryStateMatches(root)
	matches, err := probe([]string{"does-not-exist"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matches {
		t.Fatalf("missing repo: got matches=true, want false")
	}
}

func TestRepositoryStateMatches_NonGitDirIsSkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	probe := RepositoryStateMatches(root)
	matches, err := probe([]string{"plain"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matches {
		t.Fatalf("non-git dir: got matches=false, want true (skipped)")
	}
}

func TestRepositoryStateMatches_EmptySlice(t *testing.T) {
	matches, err := RepositoryStateMatches(t.TempDir())(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matches {
		t.Fatalf("empty slice: got matches=false, want true")
	}
}

func TestEvidenceExists_ReportsMissingAndPresent(t *testing.T) {
	root := t.TempDir()

	// Write a ledger with one record, mirroring
	// .punakawan/evidence/<runID>/records.jsonl.
	ledgerDir := filepath.Join(root, ".punakawan", "evidence", "run-1")
	if err := os.MkdirAll(ledgerDir, 0o755); err != nil {
		t.Fatalf("mkdir ledger: %v", err)
	}
	ledger := filepath.Join(ledgerDir, "records.jsonl")
	if err := os.WriteFile(ledger, []byte(`{"id":"ev-present","run_id":"run-1"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write ledger: %v", err)
	}

	missing, err := EvidenceExists(root)([]string{"ev-present", "ev-fabricated"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missing) != 1 || missing[0] != "ev-fabricated" {
		t.Fatalf("got missing=%v, want [ev-fabricated]", missing)
	}
}

func TestEvidenceExists_NoTreeReportsAllMissing(t *testing.T) {
	root := t.TempDir()

	missing, err := EvidenceExists(root)([]string{"ev-a", "ev-b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missing) != 2 {
		t.Fatalf("got missing=%v, want both ids missing", missing)
	}
}

func TestEvidenceExists_EmptySlice(t *testing.T) {
	missing, err := EvidenceExists(t.TempDir())(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if missing != nil {
		t.Fatalf("empty slice: got missing=%v, want nil", missing)
	}
}

func TestTaskIsCurrent_ConservativeTrue(t *testing.T) {
	current, err := TaskIsCurrent(t.TempDir())("task-anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !current {
		t.Fatalf("TaskIsCurrent: got false, want documented default true")
	}
}
