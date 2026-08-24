package mcpserver

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/deliverysummary"
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
	Comment               string   `json:"comment,omitempty" jsonschema:"comment body in Markdown (confirmed working; converted to ADF). Do NOT use old Jira wiki markup - h3./{{code}} render literally. Style: clear, concise, plain language - short sentences, everyday words, no jargon, no filler, no hype, no theatrical or mystical phrasing. State what happened or what's needed and why it matters, nothing more."`
	// TransitionToStatus, when given, moves the issue to the Jira workflow
	// status with this name (e.g. "In Progress", "Done") - matched
	// case-insensitively against the issue's currently available
	// transitions' target status names (falling back to the transition
	// names themselves, since some workflows name a transition differently
	// from the status it lands on). This folds status movement into the
	// same call as the worklog/comment so a caller doing "I did work, log
	// it, and move the ticket" does not need a separate tool plus its own
	// getTransitionsForJiraIssue lookup.
	TransitionToStatus string `json:"transition_to_status,omitempty" jsonschema:"move the issue to this workflow status (matched case-insensitively against available transitions), e.g. 'In Progress' or 'Done'"`
	// PrUrl, when given, is rendered into the posted comment's canonical
	// Links section alongside this run's test/commit/risk counts - there is
	// no other way for this tool to learn a PR's URL, since it has no
	// repo_id/pull_request_number of its own to look one up by.
	PrUrl       string `json:"pr_url,omitempty" jsonschema:"the pull request URL this progress update relates to, if one exists yet"`
	RequestedBy string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
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
	// RemainingEstimateHours is the remaining estimate written alongside
	// EstimateHours - see remainingEstimateHours for why this is always set
	// explicitly rather than left for Jira to derive on its own.
	RemainingEstimateHours float64 `json:"remaining_estimate_hours,omitempty"`
	WorklogAdded           bool    `json:"worklog_added"`
	CommentPosted          bool    `json:"comment_posted"`
	Transitioned           bool    `json:"transitioned"`
	TransitionedTo         string  `json:"transitioned_to,omitempty"`
	// TransitionSkipReason explains why Transitioned is false despite
	// TransitionToStatus having been given - e.g. no available transition
	// matches, distinct from "no transition was requested" (both leave
	// Transitioned false, so this is left empty in the latter case).
	TransitionSkipReason string `json:"transition_skip_reason,omitempty"`
	// FailedStep/FailedError report a partial success (punokawan-4tw): when
	// an earlier sub-write in this call succeeded but a later one failed,
	// these name the failed step ("estimate", "worklog", "comment",
	// "transition") and carry its error, and the call still returns as a
	// non-error result so the caller sees exactly what was applied and does
	// not re-run the whole tool (which would duplicate the non-dedup
	// worklog/comment writes). The failed step is queued for retry via the
	// adapter sync queue; see list_jira_sync_queue.
	FailedStep  string `json:"failed_step,omitempty"`
	FailedError string `json:"failed_error,omitempty"`
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

		summary := buildDeliverySummary(ctx, a, in.RunId, "", "", "", in.PrUrl, "")
		out, err := updateJiraTaskProgress(ctx, req, gate, cfg, summary, in)
		return nil, out, err
	}
}

// updateJiraTaskProgress is updateJiraTaskProgressHandler's core logic,
// split out so it can be tested against a Gate built from a fake caller
// (mirroring internal/adapters/gate_test.go's pattern) instead of a real
// spawned adapter process, which would require live Jira credentials.
// summary is the canonical test/commit/risk/link block, folded
// into the posted comment (if any) rather than restated in caller prose.
func updateJiraTaskProgress(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, cfg *jiraworkflow.Config, summary deliverysummary.Summary, in UpdateJiraTaskProgressInput) (UpdateJiraTaskProgressOutput, error) {
	var out UpdateJiraTaskProgressOutput
	requestedBy, err := validateRequestedBy(in.RequestedBy)
	if err != nil {
		return out, err
	}

	// anySucceeded tracks whether an earlier sub-write in this call already
	// applied. On a later failure, recordPartialFailure uses it to decide
	// between surfacing an ordinary error (nothing applied yet) and returning
	// a non-error partial-success result (punokawan-4tw).
	anySucceeded := false

	estimateHours, hasEstimate, estimateSkipReason := resolveEstimateHours(cfg, in)
	out.EstimateSkipReason = estimateSkipReason
	if hasEstimate {
		remainingHours := remainingEstimateHours(estimateHours, in.WorklogHours)
		if _, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "atlassian.editJiraIssueFields", map[string]any{
			"issueIdOrKey": in.IssueIdOrKey,
			"fields": map[string]any{
				"timetracking": map[string]any{
					"originalEstimate":  formatJiraDuration(estimateHours),
					"remainingEstimate": formatJiraDuration(remainingHours),
				},
			},
		}, requestedBy); err != nil {
			return out, recordPartialFailure(&out.FailedStep, &out.FailedError, anySucceeded, "estimate", fmt.Errorf("mcpserver: update original estimate: %w", err))
		}
		out.EstimateUpdated = true
		out.EstimateHours = estimateHours
		out.RemainingEstimateHours = remainingHours
		anySucceeded = true
	}

	if in.WorklogHours != nil {
		if _, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "atlassian.addWorklog", map[string]any{
			"issueIdOrKey":     in.IssueIdOrKey,
			"timeSpentSeconds": int(math.Round(*in.WorklogHours * 3600)),
		}, requestedBy); err != nil {
			return out, recordPartialFailure(&out.FailedStep, &out.FailedError, anySucceeded, "worklog", fmt.Errorf("mcpserver: add worklog: %w", err))
		}
		out.WorklogAdded = true
		anySucceeded = true
	}

	if in.Comment != "" {
		commentBody := in.Comment
		if section := summary.Section("###"); section != "" {
			commentBody += "\n\n" + section
		}
		if _, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "atlassian.addJiraComment", map[string]any{
			"issueIdOrKey": in.IssueIdOrKey,
			"commentBody":  commentBody,
		}, requestedBy); err != nil {
			return out, recordPartialFailure(&out.FailedStep, &out.FailedError, anySucceeded, "comment", fmt.Errorf("mcpserver: post comment: %w", err))
		}
		out.CommentPosted = true
		anySucceeded = true
	}

	if in.TransitionToStatus != "" {
		transitioned, _, transitionedTo, skipReason, err := transitionIssueToStatus(
			ctx, req, gate, in.RunId, in.IssueIdOrKey, in.TransitionToStatus, requestedBy,
		)
		if err != nil {
			return out, recordPartialFailure(&out.FailedStep, &out.FailedError, anySucceeded, "transition", fmt.Errorf("mcpserver: transition issue: %w", err))
		}
		out.Transitioned = transitioned
		out.TransitionedTo = transitionedTo
		out.TransitionSkipReason = skipReason
	}

	return out, nil
}

// transitionIssueToStatus resolves targetStatusName against the issue's
// currently available transitions (matched case-insensitively against each
// transition's target status name, falling back to the transition's own
// name, since some workflows name a transition differently from the status
// it lands on) and, on a match, fires it. No match is reported via
// skipReason rather than an error - the issue may simply not have that
// status reachable from its current state, which is a normal outcome for a
// caller to branch on, not a failure of the call itself.
func transitionIssueToStatus(
	ctx context.Context,
	req *mcp.CallToolRequest,
	gate *adapters.Gate,
	runID string,
	issueIDOrKey string,
	targetStatusName string,
	requestedBy protocol.ApprovalRecordRequestedBy,
) (transitioned bool, transitionID string, transitionedTo string, skipReason string, err error) {
	raw, err := invokeAdapterOperation(ctx, req, gate, runID, "atlassian.getTransitionsForJiraIssue", map[string]any{
		"issueIdOrKey": issueIDOrKey,
	}, requestedBy)
	if err != nil {
		return false, "", "", "", fmt.Errorf("list available transitions: %w", err)
	}

	transitions, err := adapters.DecodeJiraTransitions(raw)
	if err != nil {
		return false, "", "", "", err
	}
	match, available, ok := adapters.MatchJiraTransition(transitions, targetStatusName)
	if !ok {
		return false, "", "", fmt.Sprintf(
			"no transition from the issue's current status reaches %q; available target statuses: %s",
			targetStatusName, strings.Join(available, ", "),
		), nil
	}

	if _, err := invokeAdapterOperation(ctx, req, gate, runID, "atlassian.transitionJiraIssue", map[string]any{
		"issueIdOrKey": issueIDOrKey,
		"transitionId": match.ID,
	}, requestedBy); err != nil {
		return false, "", "", "", fmt.Errorf("fire transition %q: %w", match.ID, err)
	}

	return true, match.ID, match.ToStatusName, "", nil
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

// remainingEstimateHours computes the remaining estimate to write alongside
// a new original estimate. Jira's own behavior when only originalEstimate is
// set is documented as unreliable (confirmed via the Atlassian developer
// community: https://community.developer.atlassian.com/t/how-to-update-timeoriginalestimate-using-the-rest-api/96650
// - "setting originalEstimate updates remainingEstimate automatically [in an
// inconsistent way]... workaround is to always set both...together") -
// rather than depend on that, this always computes and writes an explicit
// value: the new original minus whatever worklog this same call is also
// adding (that time will not yet be reflected in Jira's own timeSpent at the
// moment this write happens).
//
// This does not account for time logged in earlier update_jira_task_progress
// calls or logged manually in the Jira UI, since knowing that would require
// fetching the issue's current timeSpent first - not done here to keep this
// a single write instead of a read-then-write. If a task accumulates several
// separate re-estimate calls over its lifetime, only the latest call's own
// worklog is subtracted.
func remainingEstimateHours(originalHours float64, worklogHours *float64) float64 {
	remaining := originalHours
	if worklogHours != nil {
		remaining -= *worklogHours
	}
	if remaining < 0 {
		return 0
	}
	return remaining
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
