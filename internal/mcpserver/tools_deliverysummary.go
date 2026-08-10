package mcpserver

import (
	"context"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/deliverysummary"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// buildDeliverySummary assembles a run's canonical deliverysummary.Summary
// from its evidence ledger, its PR review findings, and (when a
// repository/branch range is known) its commit history, per punokawan-xu7m.
// repoPath/baseBranch/headBranch may be "" when a call site has no single
// repository in view (e.g. update_jira_task_progress, record_work_outcome);
// Gather leaves Commits empty in that case rather than guessing one.
//
// Gathering this data is best-effort telemetry, not a precondition for the
// caller's actual operation (creating a PR, posting a Jira comment,
// recording an outcome): a failure to open the ledger returns an empty
// Summary (which renders no canonical section at all, per
// deliverysummary.Summary.HasContent) instead of failing the call.
func buildDeliverySummary(ctx context.Context, a *app.App, runID, repoPath, baseBranch, headBranch, prURL, jiraURL string) deliverysummary.Summary {
	var risks []protocol.ReviewFinding
	if a.PrReviews != nil {
		if records, err := a.PrReviews.ForRun(runID); err == nil {
			for _, rec := range records {
				risks = append(risks, rec.Findings...)
			}
		}
	}

	in, err := deliverysummary.Gather(ctx, deliverysummary.GatherInput{
		WorkspaceRoot: a.Workspace.Root,
		RunId:         runID,
		Inspector:     a.Inspector,
		RepoPath:      repoPath,
		BaseBranch:    baseBranch,
		HeadBranch:    headBranch,
		Risks:         risks,
		PrUrl:         prURL,
		JiraUrl:       jiraURL,
	})
	if err != nil {
		return deliverysummary.Summary{RunId: runID, PrUrl: prURL, JiraUrl: jiraURL}
	}
	return deliverysummary.Build(in)
}
