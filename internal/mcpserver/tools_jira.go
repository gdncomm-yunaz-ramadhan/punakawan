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

// --- list_jira_subtasks ----------------------------------------------------

// ListJiraSubtasksInput is list_jira_subtasks's input.
type ListJiraSubtasksInput struct {
	RunId        string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session. This is a read, so no approval is needed either way."`
	IssueIdOrKey string `json:"issue_id_or_key" jsonschema:"the parent Jira issue key or id whose existing subtasks (children) to list, e.g. PAY-1842"`
	RequestedBy  string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// JiraSubtask is one child issue returned by list_jira_subtasks.
type JiraSubtask struct {
	Key     string `json:"key"`
	Summary string `json:"summary,omitempty"`
	Status  string `json:"status,omitempty"`
}

// ListJiraSubtasksOutput is list_jira_subtasks's output. Parent* fields echo
// the queried issue so a caller can confirm it resolved the intended ticket
// before picking a subtask key to log work against.
type ListJiraSubtasksOutput struct {
	ParentKey     string        `json:"parent_key"`
	ParentSummary string        `json:"parent_summary,omitempty"`
	ParentStatus  string        `json:"parent_status,omitempty"`
	Subtasks      []JiraSubtask `json:"subtasks"`
	SubtaskCount  int           `json:"subtask_count"`
}

func listJiraSubtasksHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListJiraSubtasksInput) (*mcp.CallToolResult, ListJiraSubtasksOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListJiraSubtasksInput) (*mcp.CallToolResult, ListJiraSubtasksOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, ListJiraSubtasksOutput{}, fmt.Errorf("mcpserver: list_jira_subtasks: %w", err)
		}
		out, err := listJiraSubtasks(ctx, req, gate, in)
		return nil, out, err
	}
}

// listJiraSubtasks fetches the parent issue's children via the same
// atlassian.getJiraIssue read ingest_jira_requirement uses, but requests only
// the subtasks/summary/status fields (not *all) since routing worklog to the
// right child needs nothing more. The adapter already normalizes
// fields.subtasks into [{key,summary,status}] (see adapter-atlassian
// normalizeJiraIssue); this tool is the missing MCP surface that exposes that
// to the connected agent, which previously had no way to enumerate a parent's
// existing subtasks and so mis-logged work on the parent.
func listJiraSubtasks(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in ListJiraSubtasksInput) (ListJiraSubtasksOutput, error) {
	var out ListJiraSubtasksOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.IssueIdOrKey) == "" {
		return out, fmt.Errorf("mcpserver: list_jira_subtasks: issue_id_or_key is required")
	}

	raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.getJiraIssue", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"fields":       []string{"subtasks", "summary", "status"},
	}, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: list_jira_subtasks: fetch %q: %w", in.IssueIdOrKey, err)
	}

	var res struct {
		Normalized struct {
			Key      string `json:"key"`
			Summary  string `json:"summary"`
			Status   string `json:"status"`
			Subtasks []struct {
				Key     string `json:"key"`
				Summary string `json:"summary"`
				Status  string `json:"status"`
			} `json:"subtasks"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return out, fmt.Errorf("mcpserver: list_jira_subtasks: decode %q: %w", in.IssueIdOrKey, err)
	}
	if res.Normalized.Key == "" {
		return out, fmt.Errorf("mcpserver: list_jira_subtasks: %q: adapter response had no normalized.key", in.IssueIdOrKey)
	}

	out.ParentKey = res.Normalized.Key
	out.ParentSummary = res.Normalized.Summary
	out.ParentStatus = res.Normalized.Status
	for _, s := range res.Normalized.Subtasks {
		out.Subtasks = append(out.Subtasks, JiraSubtask{Key: s.Key, Summary: s.Summary, Status: s.Status})
	}
	out.SubtaskCount = len(out.Subtasks)
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

// --- list_jira_linked_issues -----------------------------------------------

// ListJiraLinkedIssuesInput is list_jira_linked_issues's input.
type ListJiraLinkedIssuesInput struct {
	RunId        string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session. This is a read, so no approval is needed either way."`
	IssueIdOrKey string `json:"issue_id_or_key" jsonschema:"the Jira issue key or id whose issue links (Blocks/Relates/etc.) to list, e.g. PAY-1842"`
	RequestedBy  string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// JiraLinkedIssue is one linked issue returned by list_jira_linked_issues.
// Direction/relationship come straight from Jira's issueLink (e.g. direction
// "outward", relationship "blocks").
type JiraLinkedIssue struct {
	Direction    string `json:"direction,omitempty"`
	Relationship string `json:"relationship,omitempty"`
	Key          string `json:"key"`
	Summary      string `json:"summary,omitempty"`
	Status       string `json:"status,omitempty"`
	IssueType    string `json:"issue_type,omitempty"`
}

// ListJiraLinkedIssuesOutput is list_jira_linked_issues's output.
type ListJiraLinkedIssuesOutput struct {
	IssueKey string            `json:"issue_key"`
	Links    []JiraLinkedIssue `json:"links"`
	Count    int               `json:"count"`
}

func listJiraLinkedIssuesHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListJiraLinkedIssuesInput) (*mcp.CallToolResult, ListJiraLinkedIssuesOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListJiraLinkedIssuesInput) (*mcp.CallToolResult, ListJiraLinkedIssuesOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, ListJiraLinkedIssuesOutput{}, fmt.Errorf("mcpserver: list_jira_linked_issues: %w", err)
		}
		out, err := listJiraLinkedIssues(ctx, req, gate, in)
		return nil, out, err
	}
}

// listJiraLinkedIssues reads the issue's links via getJiraIssue (fields
// issuelinks/summary/status only), which the adapter already normalizes into
// {direction, relationship, issue{key,summary,status,issueType}}.
func listJiraLinkedIssues(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in ListJiraLinkedIssuesInput) (ListJiraLinkedIssuesOutput, error) {
	var out ListJiraLinkedIssuesOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.IssueIdOrKey) == "" {
		return out, fmt.Errorf("mcpserver: list_jira_linked_issues: issue_id_or_key is required")
	}

	raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.getJiraIssue", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"fields":       []string{"issuelinks", "summary", "status"},
	}, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: list_jira_linked_issues: fetch %q: %w", in.IssueIdOrKey, err)
	}

	var res struct {
		Normalized struct {
			Key   string `json:"key"`
			Links []struct {
				Direction    string `json:"direction"`
				Relationship string `json:"relationship"`
				Issue        struct {
					Key       string `json:"key"`
					Summary   string `json:"summary"`
					Status    string `json:"status"`
					IssueType string `json:"issueType"`
				} `json:"issue"`
			} `json:"links"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return out, fmt.Errorf("mcpserver: list_jira_linked_issues: decode %q: %w", in.IssueIdOrKey, err)
	}
	if res.Normalized.Key == "" {
		return out, fmt.Errorf("mcpserver: list_jira_linked_issues: %q: adapter response had no normalized.key", in.IssueIdOrKey)
	}

	out.IssueKey = res.Normalized.Key
	for _, l := range res.Normalized.Links {
		out.Links = append(out.Links, JiraLinkedIssue{
			Direction:    l.Direction,
			Relationship: l.Relationship,
			Key:          l.Issue.Key,
			Summary:      l.Issue.Summary,
			Status:       l.Issue.Status,
			IssueType:    l.Issue.IssueType,
		})
	}
	out.Count = len(out.Links)
	return out, nil
}

// --- list_jira_comments ----------------------------------------------------

// ListJiraCommentsInput is list_jira_comments's input.
type ListJiraCommentsInput struct {
	RunId        string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session. This is a read, so no approval is needed either way."`
	IssueIdOrKey string `json:"issue_id_or_key" jsonschema:"the Jira issue key or id whose comments to list"`
	StartAt      int    `json:"start_at,omitempty" jsonschema:"0-based paging offset, default 0"`
	MaxResults   int    `json:"max_results,omitempty" jsonschema:"max comments to return, default 20, capped at 100"`
	RequestedBy  string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// JiraComment is one comment returned by list_jira_comments. Body is plain
// text extracted from Jira's ADF, not the raw ADF document, to keep output
// concise.
type JiraComment struct {
	Id      string `json:"id"`
	Author  string `json:"author,omitempty"`
	Body    string `json:"body,omitempty"`
	Created string `json:"created,omitempty"`
	Updated string `json:"updated,omitempty"`
}

// ListJiraCommentsOutput is list_jira_comments's output. Total is Jira's
// server-side total when known, so a caller can tell whether more pages exist
// beyond the returned slice.
type ListJiraCommentsOutput struct {
	IssueKey string        `json:"issue_key"`
	Comments []JiraComment `json:"comments"`
	StartAt  int           `json:"start_at"`
	Returned int           `json:"returned"`
	Total    *int          `json:"total,omitempty"`
}

func listJiraCommentsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListJiraCommentsInput) (*mcp.CallToolResult, ListJiraCommentsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListJiraCommentsInput) (*mcp.CallToolResult, ListJiraCommentsOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, ListJiraCommentsOutput{}, fmt.Errorf("mcpserver: list_jira_comments: %w", err)
		}
		out, err := listJiraComments(ctx, req, gate, in)
		return nil, out, err
	}
}

func listJiraComments(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in ListJiraCommentsInput) (ListJiraCommentsOutput, error) {
	var out ListJiraCommentsOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.IssueIdOrKey) == "" {
		return out, fmt.Errorf("mcpserver: list_jira_comments: issue_id_or_key is required")
	}

	params := map[string]any{"issueIdOrKey": in.IssueIdOrKey}
	if in.StartAt > 0 {
		params["startAt"] = in.StartAt
	}
	if in.MaxResults > 0 {
		params["maxResults"] = in.MaxResults
	}
	raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.getJiraComments", params, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: list_jira_comments: fetch %q: %w", in.IssueIdOrKey, err)
	}

	var res struct {
		Comments []struct {
			Id      string `json:"id"`
			Author  string `json:"author"`
			Body    string `json:"body"`
			Created string `json:"created"`
			Updated string `json:"updated"`
		} `json:"comments"`
		Page struct {
			StartAt  int  `json:"startAt"`
			Returned int  `json:"returned"`
			Total    *int `json:"total"`
		} `json:"page"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return out, fmt.Errorf("mcpserver: list_jira_comments: decode %q: %w", in.IssueIdOrKey, err)
	}

	out.IssueKey = in.IssueIdOrKey
	for _, c := range res.Comments {
		out.Comments = append(out.Comments, JiraComment{
			Id: c.Id, Author: c.Author, Body: c.Body, Created: c.Created, Updated: c.Updated,
		})
	}
	out.StartAt = res.Page.StartAt
	out.Returned = res.Page.Returned
	out.Total = res.Page.Total
	return out, nil
}

// --- add_jira_comment ------------------------------------------------------

// AddJiraCommentInput is add_jira_comment's input.
type AddJiraCommentInput struct {
	RunId        string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session (no workflow-run/requirement/task/capsule ceremony)"`
	IssueIdOrKey string `json:"issue_id_or_key" jsonschema:"the Jira issue key or id to comment on. To reply to an existing comment, post a new comment here referencing it - Jira issue comments are a flat list, not threaded."`
	Body         string `json:"body" jsonschema:"comment body in Markdown (confirmed working; converted to ADF). Do NOT use old Jira wiki markup - h3./{{code}} render literally."`
	RequestedBy  string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// AddJiraCommentOutput is add_jira_comment's output.
type AddJiraCommentOutput struct {
	Posted       bool   `json:"posted"`
	IssueIdOrKey string `json:"issue_id_or_key"`
	CommentId    string `json:"comment_id,omitempty"`
}

func addJiraCommentHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, AddJiraCommentInput) (*mcp.CallToolResult, AddJiraCommentOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in AddJiraCommentInput) (*mcp.CallToolResult, AddJiraCommentOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, AddJiraCommentOutput{}, fmt.Errorf("mcpserver: add_jira_comment: %w", err)
		}
		out, err := addJiraComment(ctx, req, gate, in)
		return nil, out, err
	}
}

// addJiraComment posts a standalone comment. It is the plain-comment primitive:
// request_jira_clarification (comment + transition) and update_jira_task_progress
// (comment bundled with estimate/worklog/transition) also post comments, but
// this is the one to reach for when a bare comment or reply is all that is
// wanted, without their extra semantics.
func addJiraComment(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in AddJiraCommentInput) (AddJiraCommentOutput, error) {
	var out AddJiraCommentOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.IssueIdOrKey) == "" {
		return out, fmt.Errorf("mcpserver: add_jira_comment: issue_id_or_key is required")
	}
	if strings.TrimSpace(in.Body) == "" {
		return out, fmt.Errorf("mcpserver: add_jira_comment: body is required")
	}

	raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.addJiraComment", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"commentBody":  in.Body,
	}, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: add_jira_comment: %w", err)
	}

	var res struct {
		CommentId string `json:"commentId"`
	}
	_ = json.Unmarshal(raw, &res) // commentId is best-effort; a missing id does not fail the post
	out.Posted = true
	out.IssueIdOrKey = in.IssueIdOrKey
	out.CommentId = res.CommentId
	return out, nil
}
