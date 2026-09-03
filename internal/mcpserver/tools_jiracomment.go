package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/jiraintegration"
)

// PostJiraCommentInput identifies an exact Jira issue or subtask and the
// free-text comment body to post to it, outside any lifecycle-template
// path.
type PostJiraCommentInput struct {
	OrchestrationID string `json:"orchestration_id"`
	JiraIssueKey    string `json:"jira_issue_key" jsonschema:"exact Jira issue or subtask key to comment on"`
	CommentBody     string `json:"comment_body" jsonschema:"free-text comment body to post verbatim"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"caller-stable id for this exact comment; reuse only when retrying this same comment, mint a new one for a different comment"`
}

type PostJiraCommentOutput struct {
	Status    string `json:"status"`
	CommentID string `json:"comment_id,omitempty"`
	IssueKey  string `json:"jira_issue_key"`
}

func postJiraCommentHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PostJiraCommentInput) (*mcp.CallToolResult, PostJiraCommentOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in PostJiraCommentInput) (*mcp.CallToolResult, PostJiraCommentOutput, error) {
		if strings.TrimSpace(in.OrchestrationID) == "" || strings.TrimSpace(in.JiraIssueKey) == "" || strings.TrimSpace(in.CommentBody) == "" || strings.TrimSpace(in.IdempotencyKey) == "" {
			return nil, PostJiraCommentOutput{}, fmt.Errorf("mcpserver: post_jira_comment requires orchestration_id, jira_issue_key, comment_body, and idempotency_key")
		}
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, PostJiraCommentOutput{}, err
		}
		outboxStore, err := a.OpenOutbox()
		if err != nil {
			return nil, PostJiraCommentOutput{}, err
		}
		svc := jiraintegration.NewService(store, a.AdapterRegistry, outboxStore, nil)
		resolved, err := svc.PostComment(ctx, in.OrchestrationID, in.JiraIssueKey, in.CommentBody, in.IdempotencyKey)
		if err != nil {
			return nil, PostJiraCommentOutput{}, fmt.Errorf("mcpserver: post_jira_comment: %w", err)
		}
		return nil, PostJiraCommentOutput{Status: "posted", CommentID: resolved.ExternalID, IssueKey: in.JiraIssueKey}, nil
	}
}
