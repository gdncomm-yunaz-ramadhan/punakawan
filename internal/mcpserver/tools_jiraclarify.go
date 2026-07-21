package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/pkg/protocol"
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
}

type jiraTransitionsResult struct {
	Transitions []struct {
		Id       string `json:"id"`
		Name     string `json:"name"`
		ToStatus struct {
			Id   string `json:"id"`
			Name string `json:"name"`
		} `json:"toStatus"`
	} `json:"transitions"`
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

		out, err := requestJiraClarification(ctx, gate, cfg, in)
		return nil, out, err
	}
}

// requestJiraClarification is requestJiraClarificationHandler's core logic,
// split out so it can be tested against a Gate built from a fake caller
// (mirroring internal/adapters/gate_test.go's pattern) instead of a real
// spawned adapter process, which would require live Jira credentials.
func requestJiraClarification(ctx context.Context, gate *adapters.Gate, cfg *jiraworkflow.Config, in RequestJiraClarificationInput) (RequestJiraClarificationOutput, error) {
	var out RequestJiraClarificationOutput
	requestedBy := protocol.ApprovalRecordRequestedBy(in.RequestedBy)

	if _, err := gate.RequestApproval(in.RunId, "atlassian.addJiraComment", requestedBy); err != nil {
		return out, fmt.Errorf("mcpserver: request approval for addJiraComment: %w", err)
	}
	if _, err := gate.Call(ctx, in.RunId, "atlassian.addJiraComment", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"commentBody":  in.CommentBody,
	}); err != nil {
		return out, fmt.Errorf("mcpserver: post clarification comment: %w", err)
	}
	out.CommentPosted = true

	if cfg.ClarificationStatus == "" {
		return out, nil
	}

	raw, err := gate.Call(ctx, in.RunId, "atlassian.getTransitionsForJiraIssue", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
	})
	if err != nil {
		return out, fmt.Errorf("mcpserver: list workflow transitions: %w", err)
	}
	var transitions jiraTransitionsResult
	if err := json.Unmarshal(raw, &transitions); err != nil {
		return out, fmt.Errorf("mcpserver: decode workflow transitions: %w", err)
	}

	var transitionID, toStatus string
	for _, t := range transitions.Transitions {
		if strings.EqualFold(t.ToStatus.Name, cfg.ClarificationStatus) {
			transitionID = t.Id
			toStatus = t.ToStatus.Name
			break
		}
	}
	if transitionID == "" {
		return out, fmt.Errorf("mcpserver: no workflow transition to configured clarification status %q found for issue %q", cfg.ClarificationStatus, in.IssueIdOrKey)
	}

	if _, err := gate.RequestApproval(in.RunId, "atlassian.transitionJiraIssue", requestedBy); err != nil {
		return out, fmt.Errorf("mcpserver: request approval for transitionJiraIssue: %w", err)
	}
	if _, err := gate.Call(ctx, in.RunId, "atlassian.transitionJiraIssue", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"transitionId": transitionID,
	}); err != nil {
		return out, fmt.Errorf("mcpserver: transition issue to clarification status: %w", err)
	}
	out.TransitionApplied = true
	out.TransitionId = transitionID
	out.ToStatus = toStatus

	return out, nil
}
