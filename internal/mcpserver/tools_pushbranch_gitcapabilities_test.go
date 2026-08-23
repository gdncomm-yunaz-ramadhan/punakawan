package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// findGitCapabilitiesProposals returns every learning proposal recorded
// against repoID's git-capabilities target, in store history order.
func findGitCapabilitiesProposals(t *testing.T, store *learning.Store, repoID string) []learning.Proposal {
	t.Helper()
	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	target := learning.GitCapabilitiesTargetId(repoID)
	var out []learning.Proposal
	for _, p := range all {
		if p.TargetId == target {
			out = append(out, p)
		}
	}
	return out
}

// TestRecordDetectedGitCapabilitiesAutoAcceptsAndIsIdempotent proves that a
// detected fact lands in learning.Store already accepted (no pending
// review needed) and classified
// detected_fact, and re-detecting the exact same remote/base/tool facts on a
// later run does not spam a second accepted proposal - it reinforces the
// existing one instead. A genuine change in the detected facts, in contrast,
// records as its own distinct proposal.
func TestRecordDetectedGitCapabilitiesAutoAcceptsAndIsIdempotent(t *testing.T) {
	a := newTestApp(t)
	store, err := a.OpenLearning()
	if err != nil {
		t.Fatalf("OpenLearning: %v", err)
	}

	branch := "main"
	provider := protocol.GitCapabilitiesProviderGithub
	caps := protocol.GitCapabilities{
		Detected:      true,
		Remotes:       []protocol.GitCapabilitiesRemotesElem{{Name: "origin", FetchUrl: "git@github.com:acme/widgets.git"}},
		Provider:      &provider,
		DefaultBranch: &branch,
		Capabilities:  protocol.GitCapabilitiesCapabilities{Push: true, CreateBranch: true, Commit: true, CreateWorktree: true},
	}

	recordDetectedGitCapabilities(a, "repo-a", caps)

	proposals := findGitCapabilitiesProposals(t, store, "repo-a")
	if len(proposals) != 1 {
		t.Fatalf("got %d proposals after first detection, want 1: %+v", len(proposals), proposals)
	}
	first := proposals[0]
	if first.Status != learning.StatusAccepted {
		t.Fatalf("Status = %q, want %q", first.Status, learning.StatusAccepted)
	}
	if first.Classification != learning.ClassificationDetectedFact {
		t.Fatalf("Classification = %q, want %q", first.Classification, learning.ClassificationDetectedFact)
	}
	if !learning.AutoAcceptable(first.Classification) {
		t.Fatalf("Classification %q is not AutoAcceptable, want it to be", first.Classification)
	}
	if first.ReviewId != "" {
		t.Fatalf("ReviewId = %q, want empty: an already-accepted detected fact needs no pending review", first.ReviewId)
	}
	if first.SupportCount != 1 {
		t.Fatalf("SupportCount = %d, want 1", first.SupportCount)
	}
	if len(first.EvidenceIds) == 0 {
		t.Fatal("EvidenceIds is empty, want the detected remote/branch facts recorded as evidence")
	}
	if first.Fingerprint == "" {
		t.Fatal("Fingerprint is empty")
	}

	// Re-detecting the exact same facts must not create a second accepted
	// proposal - it should reinforce the existing one.
	recordDetectedGitCapabilities(a, "repo-a", caps)

	proposals = findGitCapabilitiesProposals(t, store, "repo-a")
	if len(proposals) != 1 {
		t.Fatalf("got %d proposals after re-detecting identical facts, want 1 (idempotent): %+v", len(proposals), proposals)
	}
	if proposals[0].Id != first.Id {
		t.Fatalf("re-detection created a new proposal id %q, want the same id %q reinforced", proposals[0].Id, first.Id)
	}
	if proposals[0].SupportCount != 2 {
		t.Fatalf("SupportCount after re-detection = %d, want 2 (reinforced, not duplicated)", proposals[0].SupportCount)
	}

	// A genuine change in the detected facts (push access lost) must record
	// as its own new proposal, not silently overwrite or get swallowed by
	// the idempotency guard.
	changed := caps
	changed.Capabilities.Push = false
	recordDetectedGitCapabilities(a, "repo-a", changed)

	proposals = findGitCapabilitiesProposals(t, store, "repo-a")
	if len(proposals) != 2 {
		t.Fatalf("got %d proposals after a genuine capability change, want 2: %+v", len(proposals), proposals)
	}

	// A different repo's identical facts must not collide with repo-a's.
	recordDetectedGitCapabilities(a, "repo-b", caps)
	if got := findGitCapabilitiesProposals(t, store, "repo-a"); len(got) != 2 {
		t.Fatalf("repo-b detection affected repo-a's proposals: got %d, want 2", len(got))
	}
	if got := findGitCapabilitiesProposals(t, store, "repo-b"); len(got) != 1 {
		t.Fatalf("got %d proposals for repo-b, want 1", len(got))
	}
}

// TestPushTaskBranchHandlerRecordsDetectedGitCapabilities proves the
// end-to-end wiring through push_task_branch itself: a real push through the
// handler leaves an accepted, detected_fact proposal behind, and running it
// again for the same unchanged repository does not add a second one.
func TestPushTaskBranchHandlerRecordsDetectedGitCapabilities(t *testing.T) {
	a := newTestApp(t)
	repoPath, err := a.RepoPath("repo-a")
	if err != nil {
		t.Fatalf("RepoPath: %v", err)
	}
	newLocalRemoteForRepo(t, repoPath)

	if _, err := a.Worktrees.RequestApproval("run-1", "repo-a", "task-1", "petruk"); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := a.Worktrees.Approve("repo-a", "task-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	wt, err := a.Worktrees.Create(context.Background(), a.Workspace.Root, repoPath, "repo-a", "task-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, out, err := pushTaskBranchHandler(a)(context.Background(), nil, PushTaskBranchInput{RunId: "run-1", RepoId: "repo-a", TaskId: "task-1"}); err != nil {
		t.Fatalf("pushTaskBranchHandler: %v", err)
	} else if !out.Pushed {
		t.Fatalf("first push: Pushed = false, Reason = %q", out.Reason)
	}

	store, err := a.OpenLearning()
	if err != nil {
		t.Fatalf("OpenLearning: %v", err)
	}
	proposals := findGitCapabilitiesProposals(t, store, "repo-a")
	if len(proposals) != 1 {
		t.Fatalf("got %d git-capabilities proposals after push_task_branch, want 1: %+v", len(proposals), proposals)
	}
	if proposals[0].Status != learning.StatusAccepted || proposals[0].Classification != learning.ClassificationDetectedFact {
		t.Fatalf("proposal = %+v, want Status=accepted Classification=detected_fact", proposals[0])
	}

	// Running push_task_branch again against the same unchanged repository
	// (nothing new to push, but detection still runs) must not add a second
	// accepted proposal.
	if _, _, err := pushTaskBranchHandler(a)(context.Background(), nil, PushTaskBranchInput{RunId: "run-1", RepoId: "repo-a", TaskId: "task-1"}); err != nil {
		t.Fatalf("pushTaskBranchHandler (second run): %v", err)
	}
	proposals = findGitCapabilitiesProposals(t, store, "repo-a")
	if len(proposals) != 1 {
		t.Fatalf("got %d git-capabilities proposals after re-running push_task_branch on an unchanged repo, want 1 (idempotent): %+v", len(proposals), proposals)
	}
	if proposals[0].SupportCount != 2 {
		t.Fatalf("SupportCount after second run = %d, want 2", proposals[0].SupportCount)
	}

	_ = wt
}
