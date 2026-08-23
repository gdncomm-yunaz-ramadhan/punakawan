package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// PushTaskBranchInput is push_task_branch's input.
type PushTaskBranchInput struct {
	RunId  string `json:"run_id"`
	RepoId string `json:"repo_id"`
	TaskId string `json:"task_id"`
	// Remote defaults to "origin".
	Remote string `json:"remote,omitempty"`
	// AllowPush is this call's user-permission override (§7.4): unset
	// (nil, the default) defers to detected capability and repository
	// policy; explicit false always wins regardless of what either of
	// those say.
	AllowPush *bool `json:"allow_push,omitempty" jsonschema:"explicit per-call push permission; false always blocks the push regardless of detected capability or repository policy"`
}

// PushTaskBranchOutput is push_task_branch's output.
type PushTaskBranchOutput struct {
	Pushed bool   `json:"pushed"`
	Reason string `json:"reason,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// pushTaskBranchHandler pushes a task's branch to its remote (§8's "push
// branch" step, ahead of create_pr), gated by §7.4's detected ∩ repository
// policy ∩ user permission merge - never a force-push (see
// WorktreeManager.PushBranch). Must run before finish_task_execution
// removes the task's worktree.
func pushTaskBranchHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PushTaskBranchInput) (*mcp.CallToolResult, PushTaskBranchOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PushTaskBranchInput) (*mcp.CallToolResult, PushTaskBranchOutput, error) {
		remote := in.Remote
		if remote == "" {
			remote = "origin"
		}

		worktreePath := gitops.WorktreePath(a.Workspace.Root, in.RepoId, in.TaskId)
		caps, err := a.Inspector.DetectCapabilities(ctx, worktreePath, remote)
		if err != nil {
			return nil, PushTaskBranchOutput{}, fmt.Errorf("mcpserver: detect git capabilities: %w", err)
		}
		recordDetectedGitCapabilities(a, in.RepoId, caps)

		userPermission := gitops.DefaultExecutionPolicy(protocol.GitExecutionPolicySourceUser)
		if in.AllowPush != nil {
			userPermission.AllowPush = *in.AllowPush
		}
		repoPolicy := gitops.DefaultExecutionPolicy(protocol.GitExecutionPolicySourceRepositoryPolicy)

		merged := gitops.MergeExecutionPolicy(caps, repoPolicy, userPermission)
		if !merged.AllowPush {
			reason := "push disallowed"
			if merged.Reason != nil {
				reason = *merged.Reason
			}
			return nil, PushTaskBranchOutput{Pushed: false, Reason: reason}, nil
		}

		wt := &gitops.Worktree{Path: worktreePath}
		branch, err := a.Worktrees.PushBranch(ctx, wt, remote)
		if err != nil {
			return nil, PushTaskBranchOutput{Pushed: false, Reason: err.Error()}, nil
		}
		return nil, PushTaskBranchOutput{Pushed: true, Branch: branch}, nil
	}
}

// recordDetectedGitCapabilities appends (or reinforces) an auto-accepted,
// detected_fact learning proposal capturing repoID's currently-detected git
// remote/base/tool facts, mirroring recordWorkflowJudgment's
// already-accepted/no-review-id shape: detection
// already happened here, so there is nothing left for a panel review to
// accept - AutoAcceptable(ClassificationDetectedFact) is true precisely
// because a directly-observed fact needs no human review before it can be
// used. Idempotent via findLearningProposalByFingerprint, keyed off
// learning.GitCapabilitiesFingerprint's stable remote/base/tool digest:
// re-detecting the same facts on a later run finds the existing proposal and
// only bumps its support count, rather than appending a fresh accepted row
// on every call; a genuine change in the detected facts (a new remote, a
// different default branch, push access gained/lost) fingerprints
// differently and records as a new proposal instead.
//
// Non-fatal by design: any failure here is logged and swallowed, never
// propagated to the caller that triggered detection - push_task_branch's
// actual push must never fail because the learning store was briefly
// unavailable.
func recordDetectedGitCapabilities(a *app.App, repoID string, caps protocol.GitCapabilities) {
	store, err := a.OpenLearning()
	if err != nil {
		slog.Warn("learning: open store to record detected git capabilities failed", "repo_id", repoID, "error", err)
		return
	}

	fp := learning.GitCapabilitiesFingerprint(a.Workspace.ID, repoID, caps)
	now := time.Now().UTC()

	existing, ok, err := findLearningProposalByFingerprint(store, fp)
	if err != nil {
		slog.Warn("learning: find prior detected git capabilities proposal failed", "repo_id", repoID, "error", err)
		return
	}
	if ok {
		existing.SupportCount++
		existing.UpdatedAt = now
		if err := store.Append(existing); err != nil {
			slog.Warn("learning: reinforce detected git capabilities proposal failed", "repo_id", repoID, "error", err)
		}
		return
	}

	lp := learning.Proposal{
		Id:             randomLocalID("learn"),
		ArtifactType:   learning.TypeMetadata,
		TargetId:       learning.GitCapabilitiesTargetId(repoID),
		Fingerprint:    fp,
		Rationale:      fmt.Sprintf("detected via git remote/branch/push inspection for repo %q", repoID),
		EvidenceIds:    gitCapabilitiesEvidence(caps),
		SupportCount:   1,
		Status:         learning.StatusAccepted,
		Classification: learning.ClassificationDetectedFact,
		Confidence:     1.0,
		CreatedBy:      "gitops.DetectCapabilities",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.Append(lp); err != nil {
		slog.Warn("learning: record detected git capabilities failed", "repo_id", repoID, "error", err)
	}
}

// gitCapabilitiesEvidence renders caps' remotes and default branch as
// human-readable evidence references for the Context Improvements inbox,
// mirroring the free-form evidence_ids convention propose_project_learning's
// callers already use.
func gitCapabilitiesEvidence(caps protocol.GitCapabilities) []string {
	evidence := make([]string, 0, len(caps.Remotes)+1)
	for _, r := range caps.Remotes {
		evidence = append(evidence, fmt.Sprintf("git:remote:%s:%s", r.Name, r.FetchUrl))
	}
	if caps.DefaultBranch != nil {
		evidence = append(evidence, "git:default_branch:"+*caps.DefaultBranch)
	}
	return evidence
}
