package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// JiraAssessmentFinding is one observation about what already exists in the
// code versus what the requirement needs it to become.
type JiraAssessmentFinding struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// JiraAssessmentOpenQuestion is one point that needs a stakeholder decision
// before the work can proceed. Important is rendered distinctly in the
// posted comment so a reviewer can tell which questions actually block
// work apart from ones that are merely worth flagging.
type JiraAssessmentOpenQuestion struct {
	Question  string `json:"question"`
	Why       string `json:"why,omitempty"`
	Important bool   `json:"important,omitempty" jsonschema:"true if this needs a stakeholder decision before the task can proceed"`
}

// JiraAssessmentTask is one subtask to create from this assessment. AiHours
// is written as the Jira original/remaining estimate, per the user's
// decision that Jira's one estimate field reflects AI-assisted
// implementation time; HumanHours is narrative only - rendered into the
// comment and the task's own description - so the reader also sees the
// manual-effort baseline and the resulting time saved, without inventing a
// second Jira field neither Jira nor the rest of the org already expects.
type JiraAssessmentTask struct {
	Summary          string         `json:"summary"`
	Plan             string         `json:"plan" jsonschema:"detailed Markdown plan of the changes this task covers"`
	AiHours          float64        `json:"ai_hours" jsonschema:"hours estimated if an AI agent implements this task - written as the Jira original and remaining estimate"`
	HumanHours       float64        `json:"human_hours" jsonschema:"hours estimated if a human implements this task manually - narrative only, never written to a Jira field"`
	AdditionalFields map[string]any `json:"additional_fields,omitempty"`
}

// SubmitJiraAssessmentInput is submit_jira_assessment's input.
type SubmitJiraAssessmentInput struct {
	RunId         string `json:"run_id"`
	IssueIdOrKey  string `json:"issue_id_or_key"`
	ProjectKey    string `json:"project_key,omitempty" jsonschema:"required if tasks is non-empty - the project subtasks are created under"`
	IssueTypeName string `json:"issue_type_name,omitempty" jsonschema:"required if tasks is non-empty - the real subtask issue-type name for this project, discovered via call_adapter_operation atlassian.getIssueTypeFieldMeta; it is not always literally 'Sub-task'"`
	// Summary is the one-paragraph overview at the top of the posted
	// comment: what exists today and what needs to change.
	Summary       string                       `json:"summary"`
	Findings      []JiraAssessmentFinding      `json:"findings,omitempty"`
	OpenQuestions []JiraAssessmentOpenQuestion `json:"open_questions,omitempty"`
	Tasks         []JiraAssessmentTask         `json:"tasks,omitempty"`
	RequestedBy   string                       `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
}

// JiraAssessmentTaskResult is one task actually created, or skipped because
// a subtask with the same normalized summary already exists (createJiraSubtask's
// own dedup, same as sync_jira_subtasks).
type JiraAssessmentTaskResult struct {
	Summary        string  `json:"summary"`
	Key            string  `json:"key,omitempty"`
	ExistingKey    string  `json:"existing_key,omitempty"`
	AiHours        float64 `json:"ai_hours,omitempty"`
	HumanHours     float64 `json:"human_hours,omitempty"`
	TimeSavedHours float64 `json:"time_saved_hours,omitempty"`
}

// SubmitJiraAssessmentOutput is submit_jira_assessment's output.
type SubmitJiraAssessmentOutput struct {
	CommentPosted bool                       `json:"comment_posted"`
	TasksCreated  []JiraAssessmentTaskResult `json:"tasks_created,omitempty"`
	TasksSkipped  []JiraAssessmentTaskResult `json:"tasks_skipped,omitempty"`
}

func submitJiraAssessmentHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitJiraAssessmentInput) (*mcp.CallToolResult, SubmitJiraAssessmentOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitJiraAssessmentInput) (*mcp.CallToolResult, SubmitJiraAssessmentOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, SubmitJiraAssessmentOutput{}, fmt.Errorf("mcpserver: submit_jira_assessment: %w", err)
		}
		out, err := submitJiraAssessment(ctx, req, gate, in)
		return nil, out, err
	}
}

// submitJiraAssessment is submitJiraAssessmentHandler's core logic, split
// out so it can be tested against a Gate built from a fake caller (mirroring
// internal/adapters/gate_test.go's pattern) instead of a real spawned
// adapter process, which would require live Jira credentials.
func submitJiraAssessment(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, in SubmitJiraAssessmentInput) (SubmitJiraAssessmentOutput, error) {
	var out SubmitJiraAssessmentOutput
	requestedBy := protocol.ApprovalRecordRequestedBy(in.RequestedBy)

	if _, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "atlassian.addJiraComment", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"commentBody":  renderJiraAssessmentComment(in),
	}, requestedBy); err != nil {
		return out, fmt.Errorf("mcpserver: post assessment comment: %w", err)
	}
	out.CommentPosted = true

	if len(in.Tasks) == 0 {
		return out, nil
	}

	byTask := make(map[string]JiraAssessmentTask, len(in.Tasks))
	candidates := make([]map[string]any, len(in.Tasks))
	for i, task := range in.Tasks {
		byTask[task.Summary] = task

		fields := map[string]any{}
		for k, v := range task.AdditionalFields {
			fields[k] = v
		}
		fields["timetracking"] = map[string]any{
			"originalEstimate":  formatJiraDuration(task.AiHours),
			"remainingEstimate": formatJiraDuration(task.AiHours),
		}
		candidates[i] = map[string]any{
			"summary":          task.Summary,
			"description":      renderJiraAssessmentTaskDescription(task),
			"additionalFields": fields,
		}
	}

	raw, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "atlassian.createJiraSubtask", map[string]any{
		"parentKey":     in.IssueIdOrKey,
		"projectKey":    in.ProjectKey,
		"issueTypeName": in.IssueTypeName,
		"candidates":    candidates,
	}, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: create assessment tasks: %w", err)
	}

	var result struct {
		Created []struct {
			Key     string `json:"key"`
			Summary string `json:"summary"`
		} `json:"created"`
		Skipped []struct {
			Summary     string `json:"summary"`
			ExistingKey string `json:"existingKey"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return out, fmt.Errorf("mcpserver: decode createJiraSubtask result: %w", err)
	}

	for _, c := range result.Created {
		task := byTask[c.Summary]
		out.TasksCreated = append(out.TasksCreated, JiraAssessmentTaskResult{
			Summary:        c.Summary,
			Key:            c.Key,
			AiHours:        task.AiHours,
			HumanHours:     task.HumanHours,
			TimeSavedHours: task.HumanHours - task.AiHours,
		})
	}
	for _, s := range result.Skipped {
		out.TasksSkipped = append(out.TasksSkipped, JiraAssessmentTaskResult{Summary: s.Summary, ExistingKey: s.ExistingKey})
	}

	return out, nil
}

// renderJiraAssessmentComment builds the Markdown comment body - real
// headings, bullet lists, and a table, per the user's ask that punakawan
// "generate a jira friendly format (table, heading, etc)" - which
// addJiraComment then converts to real ADF (see operations.ts's markdownAdf)
// instead of Jira showing the literal Markdown syntax.
func renderJiraAssessmentComment(in SubmitJiraAssessmentInput) string {
	var b strings.Builder
	b.WriteString("## Assessment\n\n")
	b.WriteString(in.Summary)
	b.WriteString("\n")

	if len(in.Findings) > 0 {
		b.WriteString("\n## Findings\n\n")
		for _, f := range in.Findings {
			fmt.Fprintf(&b, "- **%s** — %s\n", f.Title, f.Detail)
		}
	}

	if len(in.OpenQuestions) > 0 {
		b.WriteString("\n## Open Questions\n\n")
		for _, q := range in.OpenQuestions {
			label := ""
			if q.Important {
				label = "**[Needs stakeholder decision]** "
			}
			if q.Why != "" {
				fmt.Fprintf(&b, "- %s%s — %s\n", label, q.Question, q.Why)
			} else {
				fmt.Fprintf(&b, "- %s%s\n", label, q.Question)
			}
		}
	}

	if len(in.Tasks) > 0 {
		b.WriteString("\n## Planned Tasks\n\n")
		b.WriteString("| Task | AI estimate | Human estimate | Time saved |\n")
		b.WriteString("| --- | --- | --- | --- |\n")
		for _, t := range in.Tasks {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
				t.Summary,
				formatJiraDuration(t.AiHours),
				formatJiraDuration(t.HumanHours),
				formatSignedHours(t.HumanHours-t.AiHours),
			)
		}
	}

	return b.String()
}

// renderJiraAssessmentTaskDescription builds one task's own description:
// its detailed plan plus the same three estimate numbers, so the estimate
// context survives on the task itself and not only in the parent comment.
func renderJiraAssessmentTaskDescription(task JiraAssessmentTask) string {
	var b strings.Builder
	b.WriteString(task.Plan)
	fmt.Fprintf(&b, "\n\n**Estimate**: %s AI-assisted, %s manual (saves %s)\n",
		formatJiraDuration(task.AiHours), formatJiraDuration(task.HumanHours), formatSignedHours(task.HumanHours-task.AiHours))
	return b.String()
}

// formatSignedHours renders a duration difference for narrative display
// only (the "time saved" column/line) - unlike formatJiraDuration, which
// assumes a non-negative value destined for an actual Jira field, this can
// be negative when the AI-assisted estimate exceeds the human one.
func formatSignedHours(hours float64) string {
	if hours < 0 {
		return "-" + formatJiraDuration(-hours)
	}
	return formatJiraDuration(hours)
}
