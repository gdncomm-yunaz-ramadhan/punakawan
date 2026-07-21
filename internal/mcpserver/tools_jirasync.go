package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CheckJiraSkippableInput is check_jira_skippable's input.
type CheckJiraSkippableInput struct {
	RequirementId string `json:"requirement_id"`
}

// CheckJiraSkippableOutput is check_jira_skippable's output.
type CheckJiraSkippableOutput struct {
	Skippable bool   `json:"skippable"`
	Status    string `json:"status"`
}

// checkJiraSkippableHandler implements the user's direct ask: "punakawan
// can skip a jira that sent back to product review." A Jira-sourced
// requirement's raw status name is stored verbatim on its KnowledgeRecord
// (adapter-atlassian's normalizeJiraIssue sets it from fields.status.name),
// so this is a read against durable knowledge, not a live Jira call: the
// calling role checks this before deciding whether to include a given
// requirement in a submit_task_graph call, rather than internal/tasks.
// GenerateGraph silently dropping items - that would complicate its
// already-shipped dependency-wiring semantics for every caller, not just
// Jira-sourced ones.
func checkJiraSkippableHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CheckJiraSkippableInput) (*mcp.CallToolResult, CheckJiraSkippableOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CheckJiraSkippableInput) (*mcp.CallToolResult, CheckJiraSkippableOutput, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, CheckJiraSkippableOutput{}, fmt.Errorf("mcpserver: check_jira_skippable: %w", err)
		}
		record, err := store.Get(in.RequirementId)
		if err != nil {
			return nil, CheckJiraSkippableOutput{}, fmt.Errorf("mcpserver: check_jira_skippable: load requirement %q: %w", in.RequirementId, err)
		}

		cfg, err := a.JiraWorkflow()
		if err != nil {
			return nil, CheckJiraSkippableOutput{}, fmt.Errorf("mcpserver: load jira workflow config: %w", err)
		}

		return nil, CheckJiraSkippableOutput{
			Skippable: cfg.ShouldSkip(record.Status),
			Status:    record.Status,
		}, nil
	}
}

// SyncJiraSubtasksCandidate is one candidate subtask to create if it does
// not already exist under the parent issue.
type SyncJiraSubtasksCandidate struct {
	Summary          string         `json:"summary"`
	Description      string         `json:"description,omitempty"`
	AdditionalFields map[string]any `json:"additional_fields,omitempty"`
}

// SyncJiraSubtasksInput is sync_jira_subtasks's input.
type SyncJiraSubtasksInput struct {
	RunId         string                      `json:"run_id"`
	ParentKey     string                      `json:"parent_key" jsonschema:"the parent Jira issue key subtasks are created under"`
	ProjectKey    string                      `json:"project_key"`
	IssueTypeName string                      `json:"issue_type_name" jsonschema:"the real subtask issue-type name for this project - discover via call_adapter_operation atlassian.getIssueTypeFieldMeta, it is not always literally 'Sub-task'"`
	Candidates    []SyncJiraSubtasksCandidate `json:"candidates"`
	RequestedBy   string                      `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
}

// SyncJiraSubtasksCreated is one subtask that was actually created.
type SyncJiraSubtasksCreated struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Status  string `json:"status,omitempty"`
}

// SyncJiraSubtasksSkipped is one candidate that was skipped because a
// matching subtask already exists.
type SyncJiraSubtasksSkipped struct {
	Summary     string `json:"summary"`
	ExistingKey string `json:"existing_key"`
}

// SyncJiraSubtasksOutput is sync_jira_subtasks's output.
type SyncJiraSubtasksOutput struct {
	Created []SyncJiraSubtasksCreated `json:"created,omitempty"`
	Skipped []SyncJiraSubtasksSkipped `json:"skipped,omitempty"`
}

// syncJiraSubtasksHandler implements the user's direct ask: "punakawan can
// also auto generate subtasks (avoid redundant task)." It wraps
// atlassian.createJiraSubtask, which itself does the dedup (exact,
// normalized-summary match against existing children) - this handler is
// just the MCP-shaped passthrough plus approval gating.
func syncJiraSubtasksHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SyncJiraSubtasksInput) (*mcp.CallToolResult, SyncJiraSubtasksOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SyncJiraSubtasksInput) (*mcp.CallToolResult, SyncJiraSubtasksOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, SyncJiraSubtasksOutput{}, fmt.Errorf("mcpserver: sync_jira_subtasks: %w", err)
		}
		out, err := syncJiraSubtasks(ctx, gate, in)
		return nil, out, err
	}
}

// syncJiraSubtasks is syncJiraSubtasksHandler's core logic, split out so it
// can be tested against a Gate built from a fake caller (mirroring
// internal/adapters/gate_test.go's pattern) instead of a real spawned
// adapter process, which would require live Jira credentials.
func syncJiraSubtasks(ctx context.Context, gate *adapters.Gate, in SyncJiraSubtasksInput) (SyncJiraSubtasksOutput, error) {
	if _, err := gate.RequestApproval(in.RunId, "atlassian.createJiraSubtask", protocol.ApprovalRecordRequestedBy(in.RequestedBy)); err != nil {
		return SyncJiraSubtasksOutput{}, fmt.Errorf("mcpserver: request approval for createJiraSubtask: %w", err)
	}

	candidates := make([]map[string]any, len(in.Candidates))
	for i, c := range in.Candidates {
		candidate := map[string]any{"summary": c.Summary}
		if c.Description != "" {
			candidate["description"] = c.Description
		}
		if len(c.AdditionalFields) > 0 {
			candidate["additionalFields"] = c.AdditionalFields
		}
		candidates[i] = candidate
	}

	raw, err := gate.Call(ctx, in.RunId, "atlassian.createJiraSubtask", map[string]any{
		"parentKey":     in.ParentKey,
		"projectKey":    in.ProjectKey,
		"issueTypeName": in.IssueTypeName,
		"candidates":    candidates,
	})
	if err != nil {
		return SyncJiraSubtasksOutput{}, fmt.Errorf("mcpserver: create jira subtasks: %w", err)
	}

	var result struct {
		Created []struct {
			Key     string `json:"key"`
			Summary string `json:"summary"`
			Status  string `json:"status"`
		} `json:"created"`
		Skipped []struct {
			Summary     string `json:"summary"`
			ExistingKey string `json:"existingKey"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return SyncJiraSubtasksOutput{}, fmt.Errorf("mcpserver: decode createJiraSubtask result: %w", err)
	}

	out := SyncJiraSubtasksOutput{}
	for _, c := range result.Created {
		out.Created = append(out.Created, SyncJiraSubtasksCreated{Key: c.Key, Summary: c.Summary, Status: c.Status})
	}
	for _, s := range result.Skipped {
		out.Skipped = append(out.Skipped, SyncJiraSubtasksSkipped{Summary: s.Summary, ExistingKey: s.ExistingKey})
	}

	return out, nil
}
