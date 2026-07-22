package gitops

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestParseRemotes(t *testing.T) {
	out := "origin\tgit@github.com:acme/widgets.git (fetch)\n" +
		"origin\tgit@github.com:acme/widgets.git (push)\n" +
		"upstream\thttps://github.com/upstream/widgets.git (fetch)\n"

	remotes := parseRemotes(out)
	if len(remotes) != 2 {
		t.Fatalf("parseRemotes = %+v, want 2 remotes", remotes)
	}
	if remotes[0].Name != "origin" || remotes[0].FetchUrl != "git@github.com:acme/widgets.git" {
		t.Fatalf("remotes[0] = %+v, want origin with the fetch URL", remotes[0])
	}
	if remotes[0].PushUrl == nil || *remotes[0].PushUrl != "git@github.com:acme/widgets.git" {
		t.Fatalf("remotes[0].PushUrl = %v, want set from the push line", remotes[0].PushUrl)
	}
	if remotes[1].Name != "upstream" || remotes[1].PushUrl != nil {
		t.Fatalf("remotes[1] = %+v, want upstream with no push line", remotes[1])
	}
}

func TestDetectProvider(t *testing.T) {
	cases := []struct {
		url  string
		want protocol.GitCapabilitiesProvider
	}{
		{"git@github.com:acme/widgets.git", protocol.GitCapabilitiesProviderGithub},
		{"https://github.com/acme/widgets.git", protocol.GitCapabilitiesProviderGithub},
		{"git@gitlab.com:acme/widgets.git", protocol.GitCapabilitiesProviderGitlab},
		{"https://bitbucket.org/acme/widgets.git", protocol.GitCapabilitiesProviderBitbucket},
		{"https://git.example.com/acme/widgets.git", protocol.GitCapabilitiesProviderGeneric},
		{"not a url at all", protocol.GitCapabilitiesProviderGeneric},
	}
	for _, c := range cases {
		if got := DetectProvider(c.url); got != c.want {
			t.Errorf("DetectProvider(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestRepositoryRootAndIsBareRepository(t *testing.T) {
	dir := newTestRepo(t)
	sup := tools.New(dir)
	insp := NewInspector(sup)
	ctx := context.Background()

	root, err := insp.RepositoryRoot(ctx, dir)
	if err != nil {
		t.Fatalf("RepositoryRoot: %v", err)
	}
	wantRoot, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if root != wantRoot {
		t.Fatalf("RepositoryRoot = %q, want %q", root, wantRoot)
	}

	bare, err := insp.IsBareRepository(ctx, dir)
	if err != nil {
		t.Fatalf("IsBareRepository: %v", err)
	}
	if bare {
		t.Fatal("IsBareRepository = true, want false for a normal working repo")
	}
}

func TestIsWorktree(t *testing.T) {
	dir := newTestRepo(t)
	worktreeDir := filepath.Join(t.TempDir(), "linked")
	runGit(t, dir, "worktree", "add", "-b", "feature", worktreeDir)

	sup := tools.New(dir, worktreeDir)
	insp := NewInspector(sup)
	ctx := context.Background()

	if isWorktree, err := insp.IsWorktree(ctx, dir); err != nil || isWorktree {
		t.Fatalf("IsWorktree(main repo) = %v, %v, want false, nil", isWorktree, err)
	}
	if isWorktree, err := insp.IsWorktree(ctx, worktreeDir); err != nil || !isWorktree {
		t.Fatalf("IsWorktree(linked worktree) = %v, %v, want true, nil", isWorktree, err)
	}
}

// newLocalRemote creates a bare repo at a fresh temp path, pushes dir's
// current branch to it as "main", and points the bare repo's own HEAD and
// dir's refs/remotes/origin/HEAD at it - reproducing what a real `git
// clone` sets up, entirely with local filesystem paths (no network).
func newLocalRemote(t *testing.T, dir string) string {
	t.Helper()
	bareDir := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, t.TempDir(), "init", "--bare", "-b", "main", bareDir)

	runGit(t, dir, "remote", "add", "origin", bareDir)
	runGit(t, dir, "push", "-u", "origin", "main")
	runGit(t, dir, "remote", "set-head", "origin", "-a")
	return bareDir
}

func TestDefaultBranch(t *testing.T) {
	dir := newTestRepo(t)
	newLocalRemote(t, dir)

	sup := tools.New(dir)
	insp := NewInspector(sup)

	branch, err := insp.DefaultBranch(context.Background(), dir, "origin")
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("DefaultBranch = %q, want main", branch)
	}
}

func TestDefaultBranchOnUnresolvableRemoteReturnsEmpty(t *testing.T) {
	dir := newTestRepo(t)
	sup := tools.New(dir)
	insp := NewInspector(sup)

	branch, err := insp.DefaultBranch(context.Background(), dir, "origin")
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if branch != "" {
		t.Fatalf("DefaultBranch = %q, want empty for a repo with no origin/HEAD", branch)
	}
}

func TestCanPushAllowedAgainstLocalBareRemote(t *testing.T) {
	dir := newTestRepo(t)
	newLocalRemote(t, dir)

	sup := tools.New(dir)
	insp := NewInspector(sup)

	ok, reason, err := insp.CanPush(context.Background(), dir, "origin", "main")
	if err != nil {
		t.Fatalf("CanPush: %v", err)
	}
	if !ok {
		t.Fatalf("CanPush = false (%q), want true against a writable local bare remote", reason)
	}
}

// TestCanPushRejectedOnDivergedHistory exercises CanPush's actual
// mechanism (a real push negotiation with the remote, rejected without
// mutating anything) using a diverged-history rejection rather than an
// auth/permission failure: a local file-path remote has no auth model of
// its own to deny against (chmod on the bare dir does not stop `git push
// --dry-run`, since dry-run's negotiation never reaches the filesystem
// write git would otherwise perform), but a non-fast-forward push is
// reliably and deterministically rejected the same way a real permission
// failure would be - CanPush cannot and does not distinguish the two.
func TestCanPushRejectedOnDivergedHistory(t *testing.T) {
	dir := newTestRepo(t)
	bareDir := newLocalRemote(t, dir)

	otherClone := filepath.Join(t.TempDir(), "other-clone")
	runGit(t, t.TempDir(), "clone", bareDir, otherClone)
	runGit(t, otherClone, "config", "user.email", "test@example.com")
	runGit(t, otherClone, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(otherClone, "from-other-clone.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write from-other-clone.txt: %v", err)
	}
	runGit(t, otherClone, "add", "from-other-clone.txt")
	runGit(t, otherClone, "commit", "-m", "diverging commit")
	runGit(t, otherClone, "push", "origin", "main")

	sup := tools.New(dir)
	insp := NewInspector(sup)

	ok, reason, err := insp.CanPush(context.Background(), dir, "origin", "main")
	if err != nil {
		t.Fatalf("CanPush: %v", err)
	}
	if ok {
		t.Fatal("CanPush = true, want false: dir's local main is behind the remote's main")
	}
	if reason == "" {
		t.Fatal("CanPush reason is empty, want the remote's rejection message")
	}
}

func TestDetectCapabilitiesEndToEnd(t *testing.T) {
	dir := newTestRepo(t)
	newLocalRemote(t, dir)
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "third commit")

	sup := tools.New(dir)
	insp := NewInspector(sup)

	caps, err := insp.DetectCapabilities(context.Background(), dir, "")
	if err != nil {
		t.Fatalf("DetectCapabilities: %v", err)
	}
	if !caps.Detected {
		t.Fatal("Detected = false, want true")
	}
	if caps.Provider == nil || *caps.Provider != protocol.GitCapabilitiesProviderGeneric {
		t.Fatalf("Provider = %v, want generic for a local file-path remote", caps.Provider)
	}
	if caps.DefaultBranch == nil || *caps.DefaultBranch != "main" {
		t.Fatalf("DefaultBranch = %v, want main", caps.DefaultBranch)
	}
	if !caps.Capabilities.Push {
		t.Fatal("Capabilities.Push = false, want true against a writable local bare remote")
	}
	if caps.Capabilities.CreatePullRequest || caps.Capabilities.ReadPullRequest || caps.Capabilities.CommentPullRequest {
		t.Fatalf("Capabilities = %+v, want all PR capabilities false (no adapter configured)", caps.Capabilities)
	}
	if len(caps.Limitations) == 0 {
		t.Fatal("Limitations is empty, want a note about missing PR-permission checking")
	}
}

func TestMergeExecutionPolicyIntersects(t *testing.T) {
	detected := protocol.GitCapabilities{Capabilities: protocol.GitCapabilitiesCapabilities{
		CreateBranch: true, CreateWorktree: true, Commit: true, Push: true, CreatePullRequest: true,
	}}
	allowAll := protocol.GitExecutionPolicy{AllowBranchCreation: true, AllowWorktreeCreation: true, AllowCommit: true, AllowPush: true, AllowPullRequestCreation: true}

	merged := MergeExecutionPolicy(detected, allowAll, allowAll)
	if !merged.AllowPush || merged.Source != protocol.GitExecutionPolicySourceDefault {
		t.Fatalf("merged = %+v, want AllowPush=true source=default when everything agrees", merged)
	}

	userNoPush := allowAll
	userNoPush.AllowPush = false
	merged = MergeExecutionPolicy(detected, allowAll, userNoPush)
	if merged.AllowPush {
		t.Fatal("AllowPush = true, want false when the user override disallows it")
	}
	if merged.Source != protocol.GitExecutionPolicySourceUser {
		t.Fatalf("Source = %q, want user (the most restrictive input)", merged.Source)
	}

	detectedNoPush := detected
	detectedNoPush.Capabilities.Push = false
	merged = MergeExecutionPolicy(detectedNoPush, allowAll, allowAll)
	if merged.AllowPush {
		t.Fatal("AllowPush = true, want false when push was never detected as possible")
	}
}
