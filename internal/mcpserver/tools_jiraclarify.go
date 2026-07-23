package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
)

// RequestJiraClarificationInput is request_jira_clarification's input.
type RequestJiraClarificationInput struct {
	RunId        string `json:"run_id"`
	IssueIdOrKey string `json:"issue_id_or_key" jsonschema:"the Jira issue key or id to comment on and transition"`
	// CommentBody is pre-rendered Markdown, e.g. produced by running
	// packages/clarification-comments's CLI (node dist/cli.js
	// <open-question.json>) over a semar_synthesis.open_questions entry.
	// This tool posts and transitions; it does not compose wording.
	CommentBody string `json:"comment_body"`
	RequestedBy string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
}

// RequestJiraClarificationOutput is request_jira_clarification's output.
type RequestJiraClarificationOutput struct {
	CommentPosted     bool   `json:"comment_posted"`
	TransitionApplied bool   `json:"transition_applied"`
	TransitionId      string `json:"transition_id,omitempty"`
	ToStatus          string `json:"to_status,omitempty"`
	// TransitionSkipReason explains why TransitionApplied is false despite a
	// clarification_status being configured - e.g. no available transition on
	// the issue's current state reaches (or is named) that status. This is a
	// soft skip, not an error: the comment still posted, and the issue may
	// simply not have that status reachable right now (punokawan-7a1).
	TransitionSkipReason string `json:"transition_skip_reason,omitempty"`
	// FailedStep/FailedError report a partial success (punokawan-4tw): if the
	// comment posted but the subsequent transition write failed, these carry
	// the failure while the call still returns as a non-error result, so the
	// caller does not re-post the (non-dedup) comment on a blind retry.
	FailedStep  string `json:"failed_step,omitempty"`
	FailedError string `json:"failed_error,omitempty"`
}

// requestJiraClarificationHandler implements the user's direct ask: "when
// punakawan challenge a jira and ask for clarification, it can comment the
// jira and update jira status to sent back to product review." It posts
// commentBody via atlassian.addJiraComment, then - only if the workspace
// has configured a clarification_status (§ jiraworkflow.Config; there is no
// universal way to know which org-specific status name means this) - looks
// up that issue's available workflow transitions and calls
// atlassian.transitionJiraIssue with the matching one, failing clearly
// (not silently) if no such transition exists from the issue's current
// state.
func requestJiraClarificationHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RequestJiraClarificationInput) (*mcp.CallToolResult, RequestJiraClarificationOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RequestJiraClarificationInput) (*mcp.CallToolResult, RequestJiraClarificationOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, RequestJiraClarificationOutput{}, fmt.Errorf("mcpserver: request_jira_clarification: %w", err)
		}
		cfg, err := a.JiraWorkflow()
		if err != nil {
			return nil, RequestJiraClarificationOutput{}, fmt.Errorf("mcpserver: load jira workflow config: %w", err)
		}

		out, err := requestJiraClarification(ctx, req, gate, cfg, in)
		return nil, out, err
	}
}

// requestJiraClarification is requestJiraClarificationHandler's core logic,
// split out so it can be tested against a Gate built from a fake caller
// (mirroring internal/adapters/gate_test.go's pattern) instead of a real
// spawned adapter process, which would require live Jira credentials.
func requestJiraClarification(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, cfg *jiraworkflow.Config, in RequestJiraClarificationInput) (RequestJiraClarificationOutput, error) {
	var out RequestJiraClarificationOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}

	if _, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "atlassian.addJiraComment", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"commentBody":  in.CommentBody,
	}, requestedBy); err != nil {
		return out, fmt.Errorf("mcpserver: post clarification comment: %w", err)
	}
	out.CommentPosted = true

	if cfg.ClarificationStatus == "" {
		return out, nil
	}

	// Reuse transitionIssueToStatus (shared with update_jira_task_progress) so
	// a configured clarification_status that names a transition, not only a
	// target status, still resolves, and a non-match is a soft skip with a
	// reason rather than a hard error (punokawan-7a1).
	transitioned, transitionID, toStatus, skipReason, err := transitionIssueToStatus(
		ctx, req, gate, in.RunId, in.IssueIdOrKey, cfg.ClarificationStatus, requestedBy,
	)
	if err != nil {
		// The comment already posted, so a transition-write failure is a
		// partial success: record it and return a non-error result rather
		// than discarding the fact that the comment went out (punokawan-4tw).
		return out, recordPartialFailure(&out.FailedStep, &out.FailedError, true, "transition", fmt.Errorf("mcpserver: transition issue to clarification status: %w", err))
	}
	out.TransitionApplied = transitioned
	out.TransitionId = transitionID
	out.ToStatus = toStatus
	out.TransitionSkipReason = skipReason

	return out, nil
}
