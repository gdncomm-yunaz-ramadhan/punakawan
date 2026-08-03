package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
)

// CreateJiraIssueInput is create_jira_issue's input. issue_type_name is a
// free-text NAME (e.g. "Bug", "Task"), not a fixed enum: issue types are
// per-site/per-project in Jira, so a caller unsure of the real names should
// discover them via call_adapter_operation atlassian.getIssueTypeFieldMeta
// first rather than guessing.
type CreateJiraIssueInput struct {
	RunId         string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session (no workflow-run/requirement/task/capsule ceremony)"`
	ProjectKey    string `json:"project_key" jsonschema:"the Jira project key to create the issue in, e.g. PAY"`
	IssueTypeName string `json:"issue_type_name" jsonschema:"the issue type NAME for this project, e.g. 'Bug' or 'Task' - names are per-site/per-project; discover the real ones via call_adapter_operation atlassian.getIssueTypeFieldMeta if unsure"`
	Summary       string `json:"summary" jsonschema:"short issue title. Style: clear, concise, plain language - short sentences, everyday words, no jargon, no filler, no hype, no theatrical or mystical phrasing. State what happened or what's needed and why it matters, nothing more."`
	Description   string `json:"description,omitempty" jsonschema:"issue body in Markdown (converted to ADF). Style: clear, concise, plain language - short sentences, everyday words, no jargon, no filler, no hype, no theatrical or mystical phrasing. State what happened or what's needed and why it matters, nothing more."`
	// ParentKey creates this issue as a child of an existing one (e.g. a
	// single ad hoc subtask). For creating several subtasks under one parent
	// with dedup against existing children, use sync_jira_subtasks instead.
	ParentKey        string         `json:"parent_key,omitempty" jsonschema:"optional: create this issue as a child of an existing issue key. For several subtasks with dedup against existing children, use sync_jira_subtasks instead."`
	AdditionalFields map[string]any `json:"additional_fields,omitempty" jsonschema:"optional extra Jira fields to set at creation time, e.g. {\"priority\": {\"name\": \"High\"}}"`
	RequestedBy      string         `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// CreateJiraIssueOutput is create_jira_issue's output.
type CreateJiraIssueOutput struct {
	Created   bool   `json:"created"`
	Key       string `json:"key"`
	Summary   string `json:"summary"`
	Status    string `json:"status,omitempty"`
	IssueType string `json:"issue_type,omitempty"`
	Url       string `json:"url,omitempty"`
}

func createJiraIssueHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreateJiraIssueInput) (*mcp.CallToolResult, CreateJiraIssueOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateJiraIssueInput) (*mcp.CallToolResult, CreateJiraIssueOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, CreateJiraIssueOutput{}, fmt.Errorf("mcpserver: create_jira_issue: %w", err)
		}
		out, err := createJiraIssueTool(ctx, req, gate, in)
		return nil, out, err
	}
}

// createJiraIssueTool wraps atlassian.createJiraIssue - the top-level create
// primitive (bug/task/any issue type, optionally as a child of an existing
// issue). sync_jira_subtasks already covers batch subtask creation with
// dedup on top of the same adapter operation; this is the single-issue,
// no-dedup entry point that was previously missing an MCP surface.
func createJiraIssueTool(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in CreateJiraIssueInput) (CreateJiraIssueOutput, error) {
	var out CreateJiraIssueOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.ProjectKey) == "" {
		return out, fmt.Errorf("mcpserver: create_jira_issue: project_key is required")
	}
	if strings.TrimSpace(in.IssueTypeName) == "" {
		return out, fmt.Errorf("mcpserver: create_jira_issue: issue_type_name is required")
	}
	if strings.TrimSpace(in.Summary) == "" {
		return out, fmt.Errorf("mcpserver: create_jira_issue: summary is required")
	}

	params := map[string]any{
		"projectKey":    in.ProjectKey,
		"issueTypeName": in.IssueTypeName,
		"summary":       in.Summary,
	}
	if in.Description != "" {
		params["description"] = in.Description
	}
	if in.ParentKey != "" {
		params["parent"] = in.ParentKey
	}
	if len(in.AdditionalFields) > 0 {
		params["additionalFields"] = in.AdditionalFields
	}

	raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.createJiraIssue", params, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: create_jira_issue: %w", err)
	}

	var res struct {
		Normalized struct {
			Key       string `json:"key"`
			Summary   string `json:"summary"`
			Status    string `json:"status"`
			IssueType string `json:"issueType"`
			Source    struct {
				Uri string `json:"uri"`
			} `json:"source"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return out, fmt.Errorf("mcpserver: create_jira_issue: decode result: %w", err)
	}
	if res.Normalized.Key == "" {
		return out, fmt.Errorf("mcpserver: create_jira_issue: adapter response had no normalized.key")
	}

	out.Created = true
	out.Key = res.Normalized.Key
	out.Summary = res.Normalized.Summary
	out.Status = res.Normalized.Status
	out.IssueType = res.Normalized.IssueType
	out.Url = res.Normalized.Source.Uri
	return out, nil
}
