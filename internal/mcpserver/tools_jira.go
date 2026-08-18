package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
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

// --- get_jira_issue --------------------------------------------------------

// GetJiraIssueInput is get_jira_issue's input. Include selects which
// sections to project out of the issue; start_at/max_results page the
// comments section and are ignored when comments are not requested.
type GetJiraIssueInput struct {
	RunId        string   `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session. This is a read, so no approval is needed either way."`
	IssueIdOrKey string   `json:"issue_id_or_key" jsonschema:"the Jira issue key or id to read, e.g. PAY-1842"`
	Include      []string `json:"include,omitempty" jsonschema:"any of subtasks, links, comments; defaults to subtasks and links, which cost one fetch together"`
	StartAt      int      `json:"start_at,omitempty" jsonschema:"comments only: 0-based paging offset, default 0"`
	MaxResults   int      `json:"max_results,omitempty" jsonschema:"comments only: max comments to return, default 20, capped at 100"`
	RequestedBy  string   `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// JiraSubtask is one child issue in get_jira_issue's subtasks section.
type JiraSubtask struct {
	Key     string `json:"key"`
	Summary string `json:"summary,omitempty"`
	Status  string `json:"status,omitempty"`
}

// GetJiraIssueOutput carries only the sections that were requested; the
// rest are omitted entirely rather than returned empty. The issue's own
// key/summary/status echo the resolved ticket so a caller can confirm it
// read the one it meant before acting on a subtask key.
type GetJiraIssueOutput struct {
	IssueKey string `json:"issue_key"`
	Summary  string `json:"summary,omitempty"`
	Status   string `json:"status,omitempty"`

	Subtasks     []JiraSubtask `json:"subtasks,omitempty"`
	SubtaskCount int           `json:"subtask_count,omitempty"`

	Links     []JiraLinkedIssue `json:"links,omitempty"`
	LinkCount int               `json:"link_count,omitempty"`

	Comments         []JiraComment `json:"comments,omitempty"`
	CommentsStartAt  int           `json:"comments_start_at,omitempty"`
	CommentsReturned int           `json:"comments_returned,omitempty"`
	CommentsTotal    *int          `json:"comments_total,omitempty"`
}

func getJiraIssueHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, GetJiraIssueInput) (*mcp.CallToolResult, GetJiraIssueOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetJiraIssueInput) (*mcp.CallToolResult, GetJiraIssueOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, GetJiraIssueOutput{}, fmt.Errorf("mcpserver: get_jira_issue: %w", err)
		}
		out, err := getJiraIssue(ctx, req, gate, in)
		return nil, out, err
	}
}

// getJiraIssue projects the requested sections out of one Jira issue.
// Subtasks and links are two projections of the same atlassian.getJiraIssue
// read (the adapter already normalizes fields.subtasks into
// [{key,summary,status}] and fields.issuelinks into {direction,
// relationship, issue{...}} - see adapter-atlassian normalizeJiraIssue), so
// asking for both costs one fetch, not two. Comments live behind a separate
// paged endpoint, so they add a second fetch only when actually requested.
func getJiraIssue(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in GetJiraIssueInput) (GetJiraIssueOutput, error) {
	var out GetJiraIssueOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.IssueIdOrKey) == "" {
		return out, fmt.Errorf("mcpserver: get_jira_issue: issue_id_or_key is required")
	}

	want, err := parseJiraIncludes(in.Include)
	if err != nil {
		return out, err
	}
	out.IssueKey = in.IssueIdOrKey

	if want["subtasks"] || want["links"] {
		// Request only the fields the requested sections actually project,
		// never *all: nothing here needs the issue's description or history.
		fields := []string{"summary", "status"}
		if want["subtasks"] {
			fields = append(fields, "subtasks")
		}
		if want["links"] {
			fields = append(fields, "issuelinks")
		}

		raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.getJiraIssue", map[string]any{
			"issueIdOrKey": in.IssueIdOrKey,
			"fields":       fields,
		}, requestedBy)
		if err != nil {
			return out, fmt.Errorf("mcpserver: get_jira_issue: fetch %q: %w", in.IssueIdOrKey, err)
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
			return out, fmt.Errorf("mcpserver: get_jira_issue: decode %q: %w", in.IssueIdOrKey, err)
		}
		if res.Normalized.Key == "" {
			return out, fmt.Errorf("mcpserver: get_jira_issue: %q: adapter response had no normalized.key", in.IssueIdOrKey)
		}

		out.IssueKey = res.Normalized.Key
		out.Summary = res.Normalized.Summary
		out.Status = res.Normalized.Status

		if want["subtasks"] {
			out.Subtasks = []JiraSubtask{}
			for _, s := range res.Normalized.Subtasks {
				out.Subtasks = append(out.Subtasks, JiraSubtask{Key: s.Key, Summary: s.Summary, Status: s.Status})
			}
			out.SubtaskCount = len(out.Subtasks)
		}
		if want["links"] {
			out.Links = []JiraLinkedIssue{}
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
			out.LinkCount = len(out.Links)
		}
	}

	if want["comments"] {
		if err := appendJiraComments(ctx, req, gate, in, requestedBy, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

// jiraIncludeSections is get_jira_issue's include vocabulary, and the
// default when a caller names none: subtasks and links come out of a single
// fetch, so defaulting to both costs no more than defaulting to either.
var jiraIncludeSections = []string{"subtasks", "links", "comments"}

func parseJiraIncludes(include []string) (map[string]bool, error) {
	want := make(map[string]bool, len(jiraIncludeSections))
	if len(include) == 0 {
		want["subtasks"] = true
		want["links"] = true
		return want, nil
	}
	for _, raw := range include {
		section := strings.ToLower(strings.TrimSpace(raw))
		if !slices.Contains(jiraIncludeSections, section) {
			return nil, fmt.Errorf("mcpserver: get_jira_issue: unknown include %q: must be one of %s", raw, strings.Join(jiraIncludeSections, ", "))
		}
		want[section] = true
	}
	return want, nil
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

// --- jira_find_sprint --------------------------------------------------------

// JiraFindSprintInput is jira_find_sprint's input. At least one of BoardId or
// ProjectKey is required: BoardId is used directly; ProjectKey is resolved to
// its board(s) first via atlassian.listJiraBoards (scrum boards only - Jira's
// sprint endpoint 400s for kanban boards, which never have sprints). This
// replaces the raw JQL "board in (X) and sprint in openSprints()" workaround a
// caller previously had to resort to just to find a sprint id without
// already knowing it.
type JiraFindSprintInput struct {
	RunId       string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session. This is a read, so no approval is needed either way."`
	BoardId     int    `json:"board_id,omitempty" jsonschema:"numeric Jira Agile board id to list sprints for; takes priority over project_key when both are set"`
	ProjectKey  string `json:"project_key,omitempty" jsonschema:"Jira project key (e.g. PAY) to resolve to its scrum board(s) when board_id is not already known; ignored if board_id is set"`
	State       string `json:"state,omitempty" jsonschema:"filter by sprint state: active, future, or closed; omit for all states"`
	Query       string `json:"query,omitempty" jsonschema:"case-insensitive substring match against sprint name; omit to return every sprint matching board/project and state"`
	MaxResults  int    `json:"max_results,omitempty" jsonschema:"max sprints to return per board, default 50"`
	RequestedBy string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong"`
}

// JiraSprint is one sprint returned by jira_find_sprint.
type JiraSprint struct {
	Id        int    `json:"id"`
	Name      string `json:"name,omitempty"`
	State     string `json:"state,omitempty"`
	BoardId   int    `json:"board_id,omitempty"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
	Goal      string `json:"goal,omitempty"`
}

// JiraFindSprintOutput is jira_find_sprint's output. BoardIds echoes which
// board(s) were actually searched - either the single board_id given, or
// every scrum board resolved from project_key - so a caller can tell which
// board a returned sprint came from without re-deriving it.
type JiraFindSprintOutput struct {
	BoardIds []int        `json:"board_ids"`
	Sprints  []JiraSprint `json:"sprints"`
	Count    int          `json:"count"`
}

func jiraFindSprintHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, JiraFindSprintInput) (*mcp.CallToolResult, JiraFindSprintOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in JiraFindSprintInput) (*mcp.CallToolResult, JiraFindSprintOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, JiraFindSprintOutput{}, fmt.Errorf("mcpserver: jira_find_sprint: %w", err)
		}
		out, err := jiraFindSprint(ctx, req, gate, in)
		return nil, out, err
	}
}

func jiraFindSprint(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in JiraFindSprintInput) (JiraFindSprintOutput, error) {
	var out JiraFindSprintOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}
	if in.BoardId <= 0 && strings.TrimSpace(in.ProjectKey) == "" {
		return out, fmt.Errorf("mcpserver: jira_find_sprint: one of board_id or project_key is required")
	}
	state := strings.ToLower(strings.TrimSpace(in.State))
	if state != "" && state != "active" && state != "future" && state != "closed" {
		return out, fmt.Errorf("mcpserver: jira_find_sprint: state must be one of active, future, closed (got %q)", in.State)
	}

	var boardIDs []int
	if in.BoardId > 0 {
		boardIDs = []int{in.BoardId}
	} else {
		raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.listJiraBoards",
			map[string]any{"projectKeyOrId": in.ProjectKey, "type": "scrum"}, requestedBy)
		if err != nil {
			return out, fmt.Errorf("mcpserver: jira_find_sprint: resolve boards for project %q: %w", in.ProjectKey, err)
		}
		var boardsRes struct {
			Boards []struct {
				Id int `json:"id"`
			} `json:"boards"`
		}
		if err := json.Unmarshal(raw, &boardsRes); err != nil {
			return out, fmt.Errorf("mcpserver: jira_find_sprint: decode boards for project %q: %w", in.ProjectKey, err)
		}
		for _, b := range boardsRes.Boards {
			boardIDs = append(boardIDs, b.Id)
		}
		if len(boardIDs) == 0 {
			return out, fmt.Errorf("mcpserver: jira_find_sprint: no scrum boards found for project %q", in.ProjectKey)
		}
	}
	out.BoardIds = boardIDs

	queryLower := strings.ToLower(strings.TrimSpace(in.Query))
	for _, boardID := range boardIDs {
		params := map[string]any{"boardId": boardID}
		if state != "" {
			params["state"] = state
		}
		if in.MaxResults > 0 {
			params["maxResults"] = in.MaxResults
		}
		raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.listJiraSprints", params, requestedBy)
		if err != nil {
			return out, fmt.Errorf("mcpserver: jira_find_sprint: list sprints for board %d: %w", boardID, err)
		}
		var res struct {
			Sprints []struct {
				Id        int    `json:"id"`
				Name      string `json:"name"`
				State     string `json:"state"`
				BoardId   int    `json:"boardId"`
				StartDate string `json:"startDate"`
				EndDate   string `json:"endDate"`
				Goal      string `json:"goal"`
			} `json:"sprints"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			return out, fmt.Errorf("mcpserver: jira_find_sprint: decode sprints for board %d: %w", boardID, err)
		}
		for _, s := range res.Sprints {
			if queryLower != "" && !strings.Contains(strings.ToLower(s.Name), queryLower) {
				continue
			}
			bid := s.BoardId
			if bid == 0 {
				bid = boardID
			}
			out.Sprints = append(out.Sprints, JiraSprint{
				Id: s.Id, Name: s.Name, State: s.State, BoardId: bid,
				StartDate: s.StartDate, EndDate: s.EndDate, Goal: s.Goal,
			})
		}
	}
	out.Count = len(out.Sprints)
	return out, nil
}

// JiraLinkedIssue is one linked issue in get_jira_issue's links section.
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

// JiraComment is one comment in get_jira_issue's comments section. Body is
// plain text extracted from Jira's ADF, not the raw ADF document, to keep
// output concise.
type JiraComment struct {
	Id      string `json:"id"`
	Author  string `json:"author,omitempty"`
	Body    string `json:"body,omitempty"`
	Created string `json:"created,omitempty"`
	Updated string `json:"updated,omitempty"`
}

// appendJiraComments fills out's comments section from the separate paged
// atlassian.getJiraComments endpoint. comments_total is Jira's server-side
// total when known, so a caller can tell whether more pages exist beyond the
// returned slice.
func appendJiraComments(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in GetJiraIssueInput, requestedBy protocol.ApprovalRecordRequestedBy, out *GetJiraIssueOutput) error {
	params := map[string]any{"issueIdOrKey": in.IssueIdOrKey}
	if in.StartAt > 0 {
		params["startAt"] = in.StartAt
	}
	if in.MaxResults > 0 {
		params["maxResults"] = in.MaxResults
	}
	raw, err := invokeAdapterOperation(ctx, req, gate, resolveRunID(in.RunId), "atlassian.getJiraComments", params, requestedBy)
	if err != nil {
		return fmt.Errorf("mcpserver: get_jira_issue: fetch comments for %q: %w", in.IssueIdOrKey, err)
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
		return fmt.Errorf("mcpserver: get_jira_issue: decode comments for %q: %w", in.IssueIdOrKey, err)
	}

	out.Comments = []JiraComment{}
	for _, c := range res.Comments {
		out.Comments = append(out.Comments, JiraComment{
			Id: c.Id, Author: c.Author, Body: c.Body, Created: c.Created, Updated: c.Updated,
		})
	}
	out.CommentsStartAt = res.Page.StartAt
	out.CommentsReturned = res.Page.Returned
	out.CommentsTotal = res.Page.Total
	return nil
}

// --- add_jira_comment ------------------------------------------------------

// AddJiraCommentInput is add_jira_comment's input.
type AddJiraCommentInput struct {
	RunId        string `json:"run_id,omitempty" jsonschema:"optional; omit for a lightweight one-off session (no workflow-run/requirement/task/capsule ceremony)"`
	IssueIdOrKey string `json:"issue_id_or_key" jsonschema:"the Jira issue key or id to comment on. To reply to an existing comment, post a new comment here referencing it - Jira issue comments are a flat list, not threaded."`
	Body         string `json:"body" jsonschema:"comment body in Markdown (confirmed working; converted to ADF). Do NOT use old Jira wiki markup - h3./{{code}} render literally. Style: clear, concise, plain language - short sentences, everyday words, no jargon, no filler, no hype, no theatrical or mystical phrasing. State what happened or what's needed and why it matters, nothing more."`
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
