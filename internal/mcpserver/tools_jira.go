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

// oneoffRunID is the run id used when a native Jira tool is called without an
// explicit run_id - the lightweight one-off path (punokawan-t6y deliverable
// 3). It keeps the approval gate working (one human approval still covers the
// session) without forcing the caller through the create_workflow_run ->
// requirement_id/task_id/capsule_id ceremony, which none of these single
// writes exercise. Passing an explicit run_id keeps the durable workflow's
// per-run approval scoping intact and is unaffected.
const oneoffRunID = "oneoff"

// defaultStoryPointsFieldId is the Jira Cloud field id most sites use for
// "Story Points". It is only a fallback: story-point custom field ids are
// per-site (and can differ per project/board), so jira_set_story_points takes
// a story_points_field_id override, and the accurate id is discoverable via
// the atlassian.getIssueTypeFieldMeta read operation.
const defaultStoryPointsFieldId = "customfield_10016"

// resolveRunID applies the lightweight one-off default: an empty run_id maps
// to oneoffRunID so a caller doesn't need a workflow run to perform a single
// approval-gated write, while any explicit run_id is preserved unchanged.
func resolveRunID(runID string) string {
	if strings.TrimSpace(runID) == "" {
		return oneoffRunID
	}
	return runID
}

// --- jira_search_user ------------------------------------------------------

// JiraSearchUserInput is jira_search_user's input.
type JiraSearchUserInput struct {
	RunId       string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session (no workflow-run/requirement/task/capsule ceremony). This is a read, so no approval is needed either way."`
	Query       string `json:"query" jsonschema:"a display name or email (or substring) to search Jira Cloud users for; the matching user's account_id is what assign/other tools need"`
	MaxResults  int    `json:"max_results,omitempty" jsonschema:"max users to return, default 20"`
	RequestedBy string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// JiraUser is one match returned by jira_search_user.
type JiraUser struct {
	AccountId    string `json:"account_id"`
	DisplayName  string `json:"display_name,omitempty"`
	EmailAddress string `json:"email_address,omitempty"`
	Active       bool   `json:"active"`
}

// JiraSearchUserOutput is jira_search_user's output.
type JiraSearchUserOutput struct {
	Users []JiraUser `json:"users"`
}

func jiraSearchUserHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, JiraSearchUserInput) (*mcp.CallToolResult, JiraSearchUserOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in JiraSearchUserInput) (*mcp.CallToolResult, JiraSearchUserOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, JiraSearchUserOutput{}, fmt.Errorf("mcpserver: jira_search_user: %w", err)
		}
		out, err := jiraSearchUser(ctx, req, gate, in)
		return nil, out, err
	}
}

func jiraSearchUser(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in JiraSearchUserInput) (JiraSearchUserOutput, error) {
	var out JiraSearchUserOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return out, fmt.Errorf("mcpserver: jira_search_user: query is required")
	}

	params := map[string]any{"query": in.Query}
	if in.MaxResults > 0 {
		params["maxResults"] = in.MaxResults
	}
	raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.searchJiraUsers", params, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: jira_search_user: %w", err)
	}

	var res struct {
		Users []struct {
			AccountId    string `json:"accountId"`
			DisplayName  string `json:"displayName"`
			EmailAddress string `json:"emailAddress"`
			Active       bool   `json:"active"`
		} `json:"users"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return out, fmt.Errorf("mcpserver: jira_search_user: decode result: %w", err)
	}
	for _, u := range res.Users {
		out.Users = append(out.Users, JiraUser{
			AccountId:    u.AccountId,
			DisplayName:  u.DisplayName,
			EmailAddress: u.EmailAddress,
			Active:       u.Active,
		})
	}
	return out, nil
}

// --- jira_link_issues ------------------------------------------------------

// JiraLinkIssuesInput is jira_link_issues's input. inward_issue/outward_issue
// map directly onto Jira's issueLink inwardIssue/outwardIssue, so the semantic
// direction follows the link type: for type "Blocks", the outward issue blocks
// the inward one; for a symmetric type like "Relates" the order is immaterial.
type JiraLinkIssuesInput struct {
	RunId        string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session (no workflow-run/requirement/task/capsule ceremony)"`
	InwardIssue  string `json:"inward_issue" jsonschema:"key of the issue on the link's inward side (e.g. for 'Blocks', the issue that is blocked by the outward one)"`
	OutwardIssue string `json:"outward_issue" jsonschema:"key of the issue on the link's outward side (e.g. for 'Blocks', the issue that blocks the inward one)"`
	LinkType     string `json:"link_type,omitempty" jsonschema:"issue link type NAME, e.g. 'Blocks' or 'Relates'; defaults to 'Relates'"`
	RequestedBy  string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// JiraLinkIssuesOutput is jira_link_issues's output.
type JiraLinkIssuesOutput struct {
	Linked       bool   `json:"linked"`
	LinkType     string `json:"link_type"`
	InwardIssue  string `json:"inward_issue"`
	OutwardIssue string `json:"outward_issue"`
}

func jiraLinkIssuesHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, JiraLinkIssuesInput) (*mcp.CallToolResult, JiraLinkIssuesOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in JiraLinkIssuesInput) (*mcp.CallToolResult, JiraLinkIssuesOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, JiraLinkIssuesOutput{}, fmt.Errorf("mcpserver: jira_link_issues: %w", err)
		}
		out, err := jiraLinkIssues(ctx, req, gate, in)
		return nil, out, err
	}
}

func jiraLinkIssues(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in JiraLinkIssuesInput) (JiraLinkIssuesOutput, error) {
	var out JiraLinkIssuesOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.InwardIssue) == "" || strings.TrimSpace(in.OutwardIssue) == "" {
		return out, fmt.Errorf("mcpserver: jira_link_issues: inward_issue and outward_issue are both required")
	}
	linkType := strings.TrimSpace(in.LinkType)
	if linkType == "" {
		linkType = "Relates"
	}

	if _, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.createIssueLink", map[string]any{
		"linkType":        linkType,
		"inwardIssueKey":  in.InwardIssue,
		"outwardIssueKey": in.OutwardIssue,
	}, requestedBy); err != nil {
		return out, fmt.Errorf("mcpserver: jira_link_issues: %w", err)
	}

	out.Linked = true
	out.LinkType = linkType
	out.InwardIssue = in.InwardIssue
	out.OutwardIssue = in.OutwardIssue
	return out, nil
}

// --- jira_set_story_points -------------------------------------------------

// JiraSetStoryPointsInput is jira_set_story_points's input.
type JiraSetStoryPointsInput struct {
	RunId        string  `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session (no workflow-run/requirement/task/capsule ceremony)"`
	IssueIdOrKey string  `json:"issue_id_or_key" jsonschema:"the Jira issue key or id to set story points on"`
	StoryPoints  float64 `json:"story_points" jsonschema:"the story-point value to write"`
	// StoryPointsFieldId overrides the custom field id. Story-point field ids
	// are per-site and can differ per project/board; discover the accurate id
	// via the atlassian.getIssueTypeFieldMeta read operation when the default
	// is wrong for a board.
	StoryPointsFieldId string `json:"story_points_field_id,omitempty" jsonschema:"Jira custom field id for Story Points (e.g. customfield_10016). Defaults to customfield_10016 (the common Jira Cloud default); override per project/board, discoverable via atlassian.getIssueTypeFieldMeta."`
	RequestedBy        string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// JiraSetStoryPointsOutput is jira_set_story_points's output.
type JiraSetStoryPointsOutput struct {
	Updated      bool    `json:"updated"`
	IssueIdOrKey string  `json:"issue_id_or_key"`
	FieldId      string  `json:"field_id"`
	StoryPoints  float64 `json:"story_points"`
}

func jiraSetStoryPointsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, JiraSetStoryPointsInput) (*mcp.CallToolResult, JiraSetStoryPointsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in JiraSetStoryPointsInput) (*mcp.CallToolResult, JiraSetStoryPointsOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, JiraSetStoryPointsOutput{}, fmt.Errorf("mcpserver: jira_set_story_points: %w", err)
		}
		out, err := jiraSetStoryPoints(ctx, req, gate, in)
		return nil, out, err
	}
}

func jiraSetStoryPoints(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in JiraSetStoryPointsInput) (JiraSetStoryPointsOutput, error) {
	var out JiraSetStoryPointsOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.IssueIdOrKey) == "" {
		return out, fmt.Errorf("mcpserver: jira_set_story_points: issue_id_or_key is required")
	}
	fieldID := strings.TrimSpace(in.StoryPointsFieldId)
	if fieldID == "" {
		fieldID = defaultStoryPointsFieldId
	}

	if _, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.editJiraIssueFields", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"fields":       map[string]any{fieldID: in.StoryPoints},
	}, requestedBy); err != nil {
		return out, fmt.Errorf("mcpserver: jira_set_story_points: %w", err)
	}

	out.Updated = true
	out.IssueIdOrKey = in.IssueIdOrKey
	out.FieldId = fieldID
	out.StoryPoints = in.StoryPoints
	return out, nil
}

// --- jira_assign_issue -----------------------------------------------------

// JiraAssignIssueInput is jira_assign_issue's input.
type JiraAssignIssueInput struct {
	RunId        string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session (no workflow-run/requirement/task/capsule ceremony)"`
	IssueIdOrKey string `json:"issue_id_or_key" jsonschema:"the Jira issue key or id to assign"`
	AccountId    string `json:"account_id" jsonschema:"the Atlassian accountId to assign to; resolve a name/email to one with jira_search_user"`
	RequestedBy  string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// JiraAssignIssueOutput is jira_assign_issue's output.
type JiraAssignIssueOutput struct {
	Assigned     bool   `json:"assigned"`
	IssueIdOrKey string `json:"issue_id_or_key"`
	AccountId    string `json:"account_id"`
}

func jiraAssignIssueHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, JiraAssignIssueInput) (*mcp.CallToolResult, JiraAssignIssueOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in JiraAssignIssueInput) (*mcp.CallToolResult, JiraAssignIssueOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, JiraAssignIssueOutput{}, fmt.Errorf("mcpserver: jira_assign_issue: %w", err)
		}
		out, err := jiraAssignIssue(ctx, req, gate, in)
		return nil, out, err
	}
}

func jiraAssignIssue(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in JiraAssignIssueInput) (JiraAssignIssueOutput, error) {
	var out JiraAssignIssueOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.IssueIdOrKey) == "" {
		return out, fmt.Errorf("mcpserver: jira_assign_issue: issue_id_or_key is required")
	}
	if strings.TrimSpace(in.AccountId) == "" {
		return out, fmt.Errorf("mcpserver: jira_assign_issue: account_id is required (use jira_search_user to resolve one)")
	}

	// Assignment goes through the same editJiraIssueFields write used elsewhere
	// (setting the assignee field to the accountId), so it inherits the same
	// approval gate as every other external Jira write rather than introducing
	// a separate adapter operation.
	if _, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.editJiraIssueFields", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"fields":       map[string]any{"assignee": map[string]any{"accountId": in.AccountId}},
	}, requestedBy); err != nil {
		return out, fmt.Errorf("mcpserver: jira_assign_issue: %w", err)
	}

	out.Assigned = true
	out.IssueIdOrKey = in.IssueIdOrKey
	out.AccountId = in.AccountId
	return out, nil
}
