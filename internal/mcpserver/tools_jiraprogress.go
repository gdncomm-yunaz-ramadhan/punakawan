package mcpserver

import (
	"context"
	"fmt"
	"math"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// UpdateJiraTaskProgressInput is update_jira_task_progress's input. All
// three actions (estimate, worklog, comment) are independently optional -
// a caller can update just one.
type UpdateJiraTaskProgressInput struct {
	RunId        string `json:"run_id"`
	IssueIdOrKey string `json:"issue_id_or_key"`
	// StoryPoints derives the original estimate via the workspace's
	// configured points-to-hours ratio (jiraworkflow.Config.EstimateHours),
	// unless OriginalEstimateHours is given explicitly.
	StoryPoints *float64 `json:"story_points,omitempty"`
	// OriginalEstimateHours, when given, overrides the points-derived
	// default - Petruk's own stated estimate takes precedence, per the
	// user's decision.
	OriginalEstimateHours *float64 `json:"original_estimate_hours,omitempty"`
	WorklogHours          *float64 `json:"worklog_hours,omitempty"`
	Comment               string   `json:"comment,omitempty"`
	RequestedBy           string   `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
}

// UpdateJiraTaskProgressOutput is update_jira_task_progress's output.
type UpdateJiraTaskProgressOutput struct {
	EstimateUpdated bool    `json:"estimate_updated"`
	EstimateHours   float64 `json:"estimate_hours,omitempty"`
	// EstimateSkipReason explains why EstimateUpdated is false despite the
	// caller having asked for an estimate (StoryPoints was given but no
	// points-to-hours ratio is configured) - left empty both when no
	// estimate was requested at all and when one was written successfully,
	// so callers can tell "nothing asked for" apart from "asked for but
	// silently couldn't be fulfilled" instead of both looking identical.
	EstimateSkipReason string `json:"estimate_skip_reason,omitempty"`
	WorklogAdded       bool   `json:"worklog_added"`
	CommentPosted      bool   `json:"comment_posted"`
}

func updateJiraTaskProgressHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, UpdateJiraTaskProgressInput) (*mcp.CallToolResult, UpdateJiraTaskProgressOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in UpdateJiraTaskProgressInput) (*mcp.CallToolResult, UpdateJiraTaskProgressOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, UpdateJiraTaskProgressOutput{}, fmt.Errorf("mcpserver: update_jira_task_progress: %w", err)
		}
		cfg, err := a.JiraWorkflow()
		if err != nil {
			return nil, UpdateJiraTaskProgressOutput{}, fmt.Errorf("mcpserver: load jira workflow config: %w", err)
		}

		out, err := updateJiraTaskProgress(ctx, gate, cfg, in)
		return nil, out, err
	}
}

// updateJiraTaskProgress is updateJiraTaskProgressHandler's core logic,
// split out so it can be tested against a Gate built from a fake caller
// (mirroring internal/adapters/gate_test.go's pattern) instead of a real
// spawned adapter process, which would require live Jira credentials.
func updateJiraTaskProgress(ctx context.Context, gate *adapters.Gate, cfg *jiraworkflow.Config, in UpdateJiraTaskProgressInput) (UpdateJiraTaskProgressOutput, error) {
	var out UpdateJiraTaskProgressOutput
	requestedBy := protocol.ApprovalRecordRequestedBy(in.RequestedBy)

	estimateHours, hasEstimate, estimateSkipReason := resolveEstimateHours(cfg, in)
	out.EstimateSkipReason = estimateSkipReason
	if hasEstimate {
		if _, err := gate.RequestApproval(in.RunId, "atlassian.editJiraIssueFields", requestedBy); err != nil {
			return out, fmt.Errorf("mcpserver: request approval for editJiraIssueFields: %w", err)
		}
		if _, err := gate.Call(ctx, in.RunId, "atlassian.editJiraIssueFields", map[string]any{
			"issueIdOrKey": in.IssueIdOrKey,
			"fields": map[string]any{
				"timetracking": map[string]any{
					"originalEstimate": formatJiraDuration(estimateHours),
				},
			},
		}); err != nil {
			return out, fmt.Errorf("mcpserver: update original estimate: %w", err)
		}
		out.EstimateUpdated = true
		out.EstimateHours = estimateHours
	}

	if in.WorklogHours != nil {
		if _, err := gate.RequestApproval(in.RunId, "atlassian.addWorklog", requestedBy); err != nil {
			return out, fmt.Errorf("mcpserver: request approval for addWorklog: %w", err)
		}
		if _, err := gate.Call(ctx, in.RunId, "atlassian.addWorklog", map[string]any{
			"issueIdOrKey":     in.IssueIdOrKey,
			"timeSpentSeconds": int(math.Round(*in.WorklogHours * 3600)),
		}); err != nil {
			return out, fmt.Errorf("mcpserver: add worklog: %w", err)
		}
		out.WorklogAdded = true
	}

	if in.Comment != "" {
		if _, err := gate.RequestApproval(in.RunId, "atlassian.addJiraComment", requestedBy); err != nil {
			return out, fmt.Errorf("mcpserver: request approval for addJiraComment: %w", err)
		}
		if _, err := gate.Call(ctx, in.RunId, "atlassian.addJiraComment", map[string]any{
			"issueIdOrKey": in.IssueIdOrKey,
			"commentBody":  in.Comment,
		}); err != nil {
			return out, fmt.Errorf("mcpserver: post comment: %w", err)
		}
		out.CommentPosted = true
	}

	return out, nil
}

// resolveEstimateHours implements the user's decision: an explicit
// OriginalEstimateHours always wins; otherwise StoryPoints is converted via
// the workspace's configured points-to-hours ratio. If neither is given,
// there is nothing to fill and hasEstimate is false with no skipReason -
// this is simply "no estimate requested", not a failure. If StoryPoints is
// given but no ratio is configured (EstimateHours' ok is false - jiraworkflow
// makes no default up), hasEstimate is still false (no invented value is
// written), but skipReason is set so the caller can tell that case apart
// from "nothing was requested" instead of both looking like a silent no-op.
func resolveEstimateHours(cfg *jiraworkflow.Config, in UpdateJiraTaskProgressInput) (hours float64, hasEstimate bool, skipReason string) {
	if in.OriginalEstimateHours != nil {
		return *in.OriginalEstimateHours, true, ""
	}
	if in.StoryPoints != nil {
		hours, ok := cfg.EstimateHours(*in.StoryPoints)
		if !ok {
			return 0, false, "story_points was given but no points_to_hours ratio is configured in jira-workflow.yaml"
		}
		return hours, true, ""
	}
	return 0, false, ""
}

// formatJiraDuration renders hours as a Jira time-tracking duration string
// (e.g. "6h 30m"), since Jira's timetracking.originalEstimate field takes a
// duration string, not a raw number, and its documented examples use whole
// hours - fractional hours are split into hours and minutes rather than
// emitted as an undocumented decimal like "6.5h".
func formatJiraDuration(hours float64) string {
	totalMinutes := int(math.Round(hours * 60))
	wholeHours := totalMinutes / 60
	minutes := totalMinutes % 60

	switch {
	case wholeHours == 0:
		return fmt.Sprintf("%dm", minutes)
	case minutes == 0:
		return fmt.Sprintf("%dh", wholeHours)
	default:
		return fmt.Sprintf("%dh %dm", wholeHours, minutes)
	}
}
