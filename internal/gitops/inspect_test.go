package gitops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/tools"
)

// runGit is test fixture setup, not the code under test: it shells out
// directly via os/exec to build a real repository for the Inspector to
// inspect.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// newTestRepo creates a real git repository in a temp dir with two commits
// and one uncommitted (modified, tracked) change plus one untracked file.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "first commit")

	other := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(other, []byte("second file\n"), 0o644); err != nil {
		t.Fatalf("write other.txt: %v", err)
	}
	runGit(t, dir, "add", "other.txt")
	runGit(t, dir, "commit", "-m", "second commit")

	// Uncommitted modification to a tracked file.
	if err := os.WriteFile(readme, []byte("hello again\n"), 0o644); err != nil {
		t.Fatalf("modify README.md: %v", err)
	}
	// Untracked file.
	untracked := filepath.Join(dir, "untracked.txt")
	if err := os.WriteFile(untracked, []byte("new\n"), 0o644); err != nil {
		t.Fatalf("write untracked.txt: %v", err)
	}

	return dir
}

func TestInspectorStatus(t *testing.T) {
	dir := newTestRepo(t)
	sup := tools.New(dir)
	insp := NewInspector(sup)

	res, err := insp.Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if res.Branch != "main" {
		t.Fatalf("Branch: got %q, want %q", res.Branch, "main")
	}
	if res.Clean {
		t.Fatal("Clean: got true, want false (repo has uncommitted changes)")
	}

	sort.Strings(res.ChangedFiles)
	want := []string{"README.md", "untracked.txt"}
	if len(res.ChangedFiles) != len(want) {
		t.Fatalf("ChangedFiles: got %v, want %v", res.ChangedFiles, want)
	}
	for idx, w := range want {
		if res.ChangedFiles[idx] != w {
			t.Fatalf("ChangedFiles: got %v, want %v", res.ChangedFiles, want)
		}
	}
}

func TestInspectorStatusClean(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "only commit")

	sup := tools.New(dir)
	insp := NewInspector(sup)

	res, err := insp.Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !res.Clean {
		t.Fatalf("Clean: got false, want true; ChangedFiles=%v", res.ChangedFiles)
	}
	if len(res.ChangedFiles) != 0 {
		t.Fatalf("ChangedFiles: got %v, want empty", res.ChangedFiles)
	}
}

func TestInspectorLog(t *testing.T) {
	dir := newTestRepo(t)
	sup := tools.New(dir)
	insp := NewInspector(sup)

	commits, err := insp.Log(context.Background(), dir, 10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("Log: got %d commits, want 2", len(commits))
	}

	// Most recent first.
	if commits[0].Subject != "second commit" {
		t.Fatalf("commits[0].Subject: got %q, want %q", commits[0].Subject, "second commit")
	}
	if commits[1].Subject != "first commit" {
		t.Fatalf("commits[1].Subject: got %q, want %q", commits[1].Subject, "first commit")
	}
	for idx, c := range commits {
		if c.SHA == "" {
			t.Fatalf("commits[%d].SHA: got empty", idx)
		}
		if c.Author != "Test User" {
			t.Fatalf("commits[%d].Author: got %q, want %q", idx, c.Author, "Test User")
		}
		if c.Date.IsZero() {
			t.Fatalf("commits[%d].Date: got zero value", idx)
		}
	}
	if !commits[0].Date.After(commits[1].Date) && !commits[0].Date.Equal(commits[1].Date) {
		t.Fatalf("expected commits[0].Date >= commits[1].Date, got %v < %v", commits[0].Date, commits[1].Date)
	}

	// Respect the limit argument.
	limited, err := insp.Log(context.Background(), dir, 1)
	if err != nil {
		t.Fatalf("Log(limit=1): %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("Log(limit=1): got %d commits, want 1", len(limited))
	}
	if limited[0].Subject != "second commit" {
		t.Fatalf("limited[0].Subject: got %q, want %q", limited[0].Subject, "second commit")
	}
}

func TestInspectorDiff(t *testing.T) {
	dir := newTestRepo(t)
	sup := tools.New(dir)
	insp := NewInspector(sup)

	diff, err := insp.Diff(context.Background(), dir, "")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "README.md") {
		t.Fatalf("Diff: expected output to mention README.md, got:\n%s", diff)
	}
	if !strings.Contains(diff, "hello again") {
		t.Fatalf("Diff: expected output to contain modified content, got:\n%s", diff)
	}

	// Diff against a specific ref (HEAD) should include the uncommitted
	// change; untracked files are never part of `git diff` output.
	diffHead, err := insp.Diff(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatalf("Diff(HEAD): %v", err)
	}
	if !strings.Contains(diffHead, "README.md") {
		t.Fatalf("Diff(HEAD): expected output to mention README.md, got:\n%s", diffHead)
	}
	if strings.Contains(diffHead, "untracked.txt") {
		t.Fatalf("Diff(HEAD): did not expect untracked file to appear, got:\n%s", diffHead)
	}
}

func TestInspectorCurrentBranch(t *testing.T) {
	dir := newTestRepo(t)
	sup := tools.New(dir)
	insp := NewInspector(sup)

	branch, err := insp.CurrentBranch(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("CurrentBranch: got %q, want %q", branch, "main")
	}
}

func TestInspectorRejectsDirOutsideAllowedRoots(t *testing.T) {
	dir := newTestRepo(t)
	other := t.TempDir()
	sup := tools.New(other) // dir is NOT within sup's allowed roots
	insp := NewInspector(sup)

	if _, err := insp.Status(context.Background(), dir); err == nil {
		t.Fatal("Status: expected error for a repo path outside the Supervisor's allowed roots")
	}
	if _, err := insp.CurrentBranch(context.Background(), dir); err == nil {
		t.Fatal("CurrentBranch: expected error for a repo path outside the Supervisor's allowed roots")
	}
}

func TestInspectorRespectsTimeout(t *testing.T) {
	dir := newTestRepo(t)
	sup := tools.New(dir)
	sup.DefaultTimeout = 1 * time.Nanosecond
	insp := NewInspector(sup)

	if _, err := insp.Status(context.Background(), dir); err == nil {
		t.Fatal("Status: expected timeout error with an effectively-zero DefaultTimeout")
	}
}
