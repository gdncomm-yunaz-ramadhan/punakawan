package delivery

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// runGitCmd runs `git <args...>` for test fixture setup and assertions,
// driving a real git binary rather than going through tools.Supervisor
// (the code under test already exercises that path; the fixture just
// needs a real repository to point it at). dir == "" runs in the test
// process's own working directory, which is fine for commands (like
// `git clone`) whose arguments are already absolute paths.
func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%q): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// worktreeFixture is a full delivery Store plus a real bare "remote"
// repository, a real local checkout cloned from it with one commit
// already pushed and tracked, a registered project whose delivery
// profile points at that checkout, and one lane routed and ready for
// CreateWorktree to act on.
type worktreeFixture struct {
	store           *Store
	orchestrationID string
	projectID       string
	laneID          string
	remoteDir       string
	localDir        string
}

func newWorktreeFixture(t *testing.T) *worktreeFixture {
	t.Helper()
	ctx := context.Background()

	remoteDir := t.TempDir()
	runGitCmd(t, remoteDir, "init", "--bare", "-b", "main")

	localDir := t.TempDir()
	runGitCmd(t, "", "clone", remoteDir, localDir)
	runGitCmd(t, localDir, "config", "user.email", "test@example.com")
	runGitCmd(t, localDir, "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(localDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGitCmd(t, localDir, "add", "README.md")
	runGitCmd(t, localDir, "commit", "-m", "initial commit")
	runGitCmd(t, localDir, "push", "-u", "origin", "main")

	store := newTestStore(t)
	proj := registerProject(t, store, "wt-project")

	if _, err := store.SetDeliveryProfile(ctx, "profile-"+NewID(), NewID(), proj.Id, ProfileInput{
		LocalPath:       localDir,
		CanonicalRemote: "origin",
		BaseBranch:      "main",
	}); err != nil {
		t.Fatalf("SetDeliveryProfile: %v", err)
	}

	orch, err := store.CreateOrchestration(ctx, "orch-"+NewID(), NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	source, err := store.CaptureRequirement(ctx, "cap-"+NewID(), orch.Id, SourceInput{Provider: "jira", ExternalID: "WT-1", Title: "seed requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	task, err := store.CreateParentTask(ctx, "task-"+NewID(), NewID(), orch.Id, "Fix the login/logout!! bug (urgent)", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	if _, err := store.RouteParentTask(ctx, "route-"+NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := store.CreateLane(ctx, "lane-"+NewID(), NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}

	return &worktreeFixture{
		store:           store,
		orchestrationID: orch.Id,
		projectID:       proj.Id,
		laneID:          lane.Id,
		remoteDir:       remoteDir,
		localDir:        localDir,
	}
}

func (f *worktreeFixture) lane(t *testing.T) *protocol.DeliveryLane {
	t.Helper()
	l, err := f.store.GetLane(context.Background(), f.orchestrationID, f.laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}
	return l
}

func TestCreateWorktreeFirstTime(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()

	beforeBranch := strings.TrimSpace(runGitCmd(t, f.localDir, "rev-parse", "--abbrev-ref", "HEAD"))
	readmeBefore, err := os.ReadFile(filepath.Join(f.localDir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md before: %v", err)
	}

	lane := f.lane(t)
	updated, err := f.store.CreateWorktree(ctx, "create-first", f.orchestrationID, f.laneID, lane.Revision)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if updated.WorktreePath == nil || *updated.WorktreePath == "" {
		t.Fatalf("expected worktree_path set, got %+v", updated)
	}
	if updated.Branch == nil || *updated.Branch == "" {
		t.Fatalf("expected branch set, got %+v", updated)
	}
	if updated.BaseSha == nil || *updated.BaseSha == "" {
		t.Fatalf("expected base_sha set, got %+v", updated)
	}
	if updated.BaseRemote == nil || *updated.BaseRemote != "origin" {
		t.Fatalf("expected base_remote = origin, got %+v", updated.BaseRemote)
	}
	if !strings.HasPrefix(*updated.Branch, "punakawan/") {
		t.Fatalf("branch %q does not have the expected punakawan/ prefix", *updated.Branch)
	}
	wantSuffix := strings.ToLower(f.laneID)
	wantSuffix = wantSuffix[len(wantSuffix)-8:]
	if !strings.HasSuffix(*updated.Branch, wantSuffix) {
		t.Fatalf("branch %q does not end with lane short id %q", *updated.Branch, wantSuffix)
	}
	if _, err := os.Stat(*updated.WorktreePath); err != nil {
		t.Fatalf("expected worktree directory to exist: %v", err)
	}

	afterBranch := strings.TrimSpace(runGitCmd(t, f.localDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if afterBranch != beforeBranch {
		t.Fatalf("base checkout's current branch changed: before=%q after=%q", beforeBranch, afterBranch)
	}
	readmeAfter, err := os.ReadFile(filepath.Join(f.localDir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md after: %v", err)
	}
	if string(readmeAfter) != string(readmeBefore) {
		t.Fatalf("base checkout's README.md changed: before=%q after=%q", readmeBefore, readmeAfter)
	}
}

func TestCreateWorktreeResumesExistingWorktree(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	lane := f.lane(t)

	first, err := f.store.CreateWorktree(ctx, "resume-first", f.orchestrationID, f.laneID, lane.Revision)
	if err != nil {
		t.Fatalf("first CreateWorktree: %v", err)
	}

	second, err := f.store.CreateWorktree(ctx, "resume-second", f.orchestrationID, f.laneID, first.Revision)
	if err != nil {
		t.Fatalf("second CreateWorktree (resume): %v", err)
	}

	if second.Revision != first.Revision {
		t.Fatalf("resume must not append a new event: first.Revision=%d second.Revision=%d", first.Revision, second.Revision)
	}
	if *second.WorktreePath != *first.WorktreePath || *second.Branch != *first.Branch {
		t.Fatalf("resume changed the lane's worktree identity: first=%+v second=%+v", first, second)
	}
}

func TestCreateWorktreeRecreatesAfterRemove(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	lane := f.lane(t)

	created, err := f.store.CreateWorktree(ctx, "recreate-create", f.orchestrationID, f.laneID, lane.Revision)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	branch := *created.Branch
	path := *created.WorktreePath

	removed, err := f.store.RemoveWorktree(ctx, "recreate-remove", f.orchestrationID, f.laneID, created.Revision)
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if removed.WorktreePath != nil {
		t.Fatalf("expected worktree_path cleared after removal, got %v", *removed.WorktreePath)
	}
	if removed.Branch == nil || *removed.Branch != branch {
		t.Fatalf("expected branch to survive removal unchanged, got %+v", removed.Branch)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected worktree directory to be gone, stat err = %v", err)
	}

	recreated, err := f.store.CreateWorktree(ctx, "recreate-again", f.orchestrationID, f.laneID, removed.Revision)
	if err != nil {
		t.Fatalf("second CreateWorktree (recreate): %v", err)
	}
	if recreated.Branch == nil || *recreated.Branch != branch {
		t.Fatalf("expected the same branch checked out again, got %+v want %q", recreated.Branch, branch)
	}
	if _, err := os.Stat(*recreated.WorktreePath); err != nil {
		t.Fatalf("expected worktree directory to exist again: %v", err)
	}
}

func TestCreateWorktreeCollisionFailsSafely(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	lane := f.lane(t)

	task, err := f.store.GetParentTask(ctx, f.orchestrationID, *lane.ParentTaskId)
	if err != nil {
		t.Fatalf("GetParentTask: %v", err)
	}
	staleBranch := laneBranchName(task.Title, f.laneID)
	// Simulate an unrelated stale branch that happens to have the exact
	// deterministic name this lane would compute.
	runGitCmd(t, f.localDir, "branch", staleBranch)

	if _, err := f.store.CreateWorktree(ctx, "collision-attempt", f.orchestrationID, f.laneID, lane.Revision); !errors.Is(err, ErrWorktreeCollision) {
		t.Fatalf("CreateWorktree against a colliding branch = %v, want ErrWorktreeCollision", err)
	}

	reloaded := f.lane(t)
	if reloaded.WorktreePath != nil || reloaded.Branch != nil {
		t.Fatalf("expected lane state untouched after a safe collision failure, got %+v", reloaded)
	}
	if reloaded.Revision != lane.Revision {
		t.Fatalf("expected no event appended on collision, revision changed from %d to %d", lane.Revision, reloaded.Revision)
	}
}

func TestRemoveWorktreeRefusesWhenDirty(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	lane := f.lane(t)

	created, err := f.store.CreateWorktree(ctx, "dirty-create", f.orchestrationID, f.laneID, lane.Revision)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(*created.WorktreePath, "scratch.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatalf("write uncommitted file into worktree: %v", err)
	}

	if _, err := f.store.RemoveWorktree(ctx, "dirty-remove", f.orchestrationID, f.laneID, created.Revision); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("RemoveWorktree on a dirty worktree = %v, want ErrWorktreeDirty", err)
	}

	if _, err := os.Stat(*created.WorktreePath); err != nil {
		t.Fatalf("expected worktree directory to still exist after refused removal: %v", err)
	}
}

func TestCommitLaneAndPushLane(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	lane := f.lane(t)

	created, err := f.store.CreateWorktree(ctx, "commit-create", f.orchestrationID, f.laneID, lane.Revision)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if err := os.WriteFile(filepath.Join(*created.WorktreePath, "work.txt"), []byte("lane work\n"), 0o644); err != nil {
		t.Fatalf("write lane work file: %v", err)
	}

	sha, err := f.store.CommitLane(ctx, f.orchestrationID, f.laneID, "lane work")
	if err != nil {
		t.Fatalf("CommitLane: %v", err)
	}
	if sha == "" {
		t.Fatal("expected a non-empty commit sha")
	}

	logOut := runGitCmd(t, *created.WorktreePath, "log", "--format=%H")
	if !strings.Contains(logOut, sha) {
		t.Fatalf("commit sha %q not found in the worktree's own history:\n%s", sha, logOut)
	}

	branch, err := f.store.PushLane(ctx, f.orchestrationID, f.laneID)
	if err != nil {
		t.Fatalf("PushLane: %v", err)
	}
	if branch != *created.Branch {
		t.Fatalf("PushLane returned branch %q, want %q", branch, *created.Branch)
	}

	lsRemote := runGitCmd(t, "", "ls-remote", f.remoteDir, "refs/heads/"+branch)
	if !strings.Contains(lsRemote, sha) {
		t.Fatalf("expected bare remote %s to have branch %s at %s, got:\n%s", f.remoteDir, branch, sha, lsRemote)
	}
}

// TestRunInLaneScopesToWorktreeAndChecksLeaseToken checks the two
// halves of worker isolation this method provides: a command only runs
// at all when the caller presents the lane's own current lease token,
// and once it runs, its working directory is the lane's own worktree -
// never the base checkout or any other lane's directory.
func TestRunInLaneScopesToWorktreeAndChecksLeaseToken(t *testing.T) {
	f := newWorktreeFixture(t)
	ctx := context.Background()
	lane := f.lane(t)

	created, err := f.store.CreateWorktree(ctx, "run-create", f.orchestrationID, f.laneID, lane.Revision)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	if _, err := f.store.SyncFrontier(ctx, "run-sync", f.orchestrationID); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}
	created, err = f.store.GetLane(ctx, f.orchestrationID, f.laneID)
	if err != nil {
		t.Fatalf("GetLane after sync: %v", err)
	}

	leased, err := f.store.GrantLease(ctx, "run-lease", f.orchestrationID, f.laneID, created.Revision, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("GrantLease: %v", err)
	}

	if _, err := f.store.RunInLane(ctx, f.orchestrationID, f.laneID, "wrong-token", "git", []string{"rev-parse", "--show-toplevel"}, 0); !errors.Is(err, ErrLeaseTokenMismatch) {
		t.Fatalf("expected ErrLeaseTokenMismatch for a wrong lease token, got %v", err)
	}

	res, err := f.store.RunInLane(ctx, f.orchestrationID, f.laneID, *leased.LeaseToken, "git", []string{"rev-parse", "--show-toplevel"}, 0)
	if err != nil {
		t.Fatalf("RunInLane: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("git rev-parse --show-toplevel failed: %s", res.Stderr)
	}

	wantRoot, err := filepath.EvalSymlinks(*leased.WorktreePath)
	if err != nil {
		t.Fatalf("EvalSymlinks(worktree path): %v", err)
	}
	gotRoot, err := filepath.EvalSymlinks(strings.TrimSpace(string(res.Stdout)))
	if err != nil {
		t.Fatalf("EvalSymlinks(command output): %v", err)
	}
	if gotRoot != wantRoot {
		t.Fatalf("command ran with toplevel %q, want the lane's own worktree %q", gotRoot, wantRoot)
	}
}

func TestLaneBranchName(t *testing.T) {
	const laneID = "01H8XG3K9QZJ4Y6N7P8R9S0TAB"
	wantSuffix := strings.ToLower(laneID)
	wantSuffix = wantSuffix[len(wantSuffix)-8:]

	cases := []struct {
		name     string
		title    string
		wantSlug string // exact expected slug; "" means only check the generic shape
	}{
		{
			name:     "simple title",
			title:    "Fix login bug",
			wantSlug: "fix-login-bug",
		},
		{
			name:     "punctuation and spaces",
			title:    "  Fix: the login/logout!! bug (urgent)  ",
			wantSlug: "fix-the-login-logout-bug-urgent",
		},
		{
			name:  "very long title truncates",
			title: "This is an extremely long parent task title that goes on and on and on and on and on",
		},
		{
			name:     "title with no alphanumeric characters",
			title:    "!!! ??? ---",
			wantSlug: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := laneBranchName(tc.title, laneID)
			if !strings.HasPrefix(got, "punakawan/") {
				t.Fatalf("laneBranchName(%q, ...) = %q, want punakawan/ prefix", tc.title, got)
			}
			if !strings.HasSuffix(got, wantSuffix) {
				t.Fatalf("laneBranchName(%q, ...) = %q, want suffix %q", tc.title, got, wantSuffix)
			}

			switch tc.name {
			case "very long title truncates":
				slugPart := strings.TrimSuffix(strings.TrimPrefix(got, "punakawan/"), "-"+wantSuffix)
				if len(slugPart) > laneBranchSlugMaxLen {
					t.Fatalf("laneBranchName(%q, ...) slug %q exceeds max length %d", tc.title, slugPart, laneBranchSlugMaxLen)
				}
				if strings.HasPrefix(slugPart, "-") || strings.HasSuffix(slugPart, "-") {
					t.Fatalf("laneBranchName(%q, ...) slug %q has a leading/trailing hyphen", tc.title, slugPart)
				}
			case "title with no alphanumeric characters":
				want := "punakawan/" + wantSuffix
				if got != want {
					t.Fatalf("laneBranchName(%q, ...) = %q, want %q (no slug at all)", tc.title, got, want)
				}
			default:
				want := "punakawan/" + tc.wantSlug + "-" + wantSuffix
				if got != want {
					t.Fatalf("laneBranchName(%q, ...) = %q, want %q", tc.title, got, want)
				}
			}
		})
	}
}
