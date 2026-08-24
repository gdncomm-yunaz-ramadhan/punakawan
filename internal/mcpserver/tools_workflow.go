package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/panel/sources"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CreateWorkflowRunInput is create_workflow_run's input.
type CreateWorkflowRunInput struct {
	RunId        string `json:"run_id" jsonschema:"stable id for the new run"`
	WorkflowName string `json:"workflow_name" jsonschema:"one of feature-delivery|requirement-review|browser-flow-capture|implementation-only|final-review"`
}

func createWorkflowRunHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreateWorkflowRunInput) (*mcp.CallToolResult, protocol.WorkflowRun, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateWorkflowRunInput) (*mcp.CallToolResult, protocol.WorkflowRun, error) {
		workflowName, err := validateWorkflowName(in.WorkflowName)
		if err != nil {
			return nil, protocol.WorkflowRun{}, err
		}

		run := workflow.New(in.RunId, a.Workspace.ID, workflowName, time.Now().UTC())
		// ROLE-012 (§50): stamp the role-config revision and an effective-role
		// settings snapshot onto the run so a historical run stays reproducible
		// even after the project role configuration is later edited. A role-config
		// read failure must never fail run creation - it is best-effort metadata,
		// so on any error we log-and-skip (leaving the fields unset).
		stampRoleConfig(a, &run)
		if err := a.Workflow.Append(run); err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: create workflow run: %w", err)
		}
		if err := sources.WriteSessionSummary(ctx, a, run); err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: create workflow run: write summary.yaml: %w", err)
		}
		return nil, run, nil
	}
}

// stampRoleConfig records the project's current role-config revision and an
// effective-role settings snapshot onto run (ROLE-012, §50). It is best-effort:
// any read/lookup failure leaves the fields unset rather than failing run
// creation, since historical role settings are metadata, not a gate. There is
// no workflow restriction to apply at creation time, so Effective reflects the
// project-level configuration for each of the four roles.
func stampRoleConfig(a *app.App, run *protocol.WorkflowRun) {
	cfg, err := roleconfig.Load(a.Workspace.Root)
	if err != nil {
		return
	}
	rev := cfg.Revision
	run.RoleConfigRevision = &rev

	settings := make(protocol.WorkflowRunEffectiveRoleSettings, len(roleconfig.AllRoles))
	for _, role := range roleconfig.AllRoles {
		rc, err := roleconfig.RoleOf(cfg, role)
		if err != nil {
			continue
		}
		eff := roleconfig.Effective(*rc, nil)
		caps := make(map[string]interface{}, len(eff.Capabilities))
		for k, v := range eff.Capabilities {
			caps[k] = v
		}
		settings[string(role)] = map[string]interface{}{
			"enabled":      eff.Enabled,
			"style":        string(eff.Style),
			"mode":         string(eff.Mode),
			"capabilities": caps,
		}
	}
	run.EffectiveRoleSettings = settings
}

// GetWorkflowStateInput is get_workflow_state's input.
type GetWorkflowStateInput struct {
	RunId string `json:"run_id"`
}

func getWorkflowStateHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, GetWorkflowStateInput) (*mcp.CallToolResult, protocol.WorkflowRun, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetWorkflowStateInput) (*mcp.CallToolResult, protocol.WorkflowRun, error) {
		run, err := a.Workflow.Get(in.RunId)
		if err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: get workflow state: %w", err)
		}
		return nil, run, nil
	}
}

// AdvanceWorkflowInput is advance_workflow's input.
type AdvanceWorkflowInput struct {
	RunId     string `json:"run_id"`
	NextState string `json:"next_state" jsonschema:"one of the states in protocol/workflow.schema.json's state enum"`
	Note      string `json:"note,omitempty"`
}

func advanceWorkflowHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, AdvanceWorkflowInput) (*mcp.CallToolResult, protocol.WorkflowRun, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in AdvanceWorkflowInput) (*mcp.CallToolResult, protocol.WorkflowRun, error) {
		run, err := a.Workflow.Get(in.RunId)
		if err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: advance workflow: %w", err)
		}

		if protocol.WorkflowRunState(in.NextState) == protocol.WorkflowRunStateCompleted {
			store, err := a.OpenKnowledge()
			if err != nil {
				return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: advance workflow: %w", err)
			}
			if err := checkNoBlockingBagongFindings(store, a, in.RunId); err != nil {
				return nil, protocol.WorkflowRun{}, err
			}
			// A context-aware run additionally requires a recorded outcome and
			// (for definition-backed runs) completed steps before it may enter
			// completed (agent-context plan §6.1).
			if err := workflow.CanComplete(run); err != nil {
				return nil, protocol.WorkflowRun{}, err
			}
		}

		run, err = workflow.Advance(run, protocol.WorkflowRunState(in.NextState), in.Note, time.Now().UTC())
		if err != nil {
			return nil, protocol.WorkflowRun{}, err
		}

		if err := a.Workflow.Append(run); err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: persist workflow advance: %w", err)
		}
		if err := sources.WriteSessionSummary(ctx, a, run); err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: advance workflow: write summary.yaml: %w", err)
		}
		return nil, run, nil
	}
}

// checkNoBlockingBagongFindings refuses completion while the run's Bagong
// review (recorded under recordID(a, "bagong", runID) by submit_lane_review
// with role bagong - there is no separate submit_bagong_review tool) has
// unresolved blocking_findings: a delivery must not be marked complete while
// a reviewer has flagged work that does not satisfy the requirement. A run
// with no Bagong review at all is allowed to complete - the review step is
// optional scaffolding like the rest of the workflow-run pipeline, not a
// mandatory gate that would break every simple run that never used it.
func checkNoBlockingBagongFindings(store *knowledge.Store, a *app.App, runID string) error {
	rec, err := store.Get(recordID(a, "bagong", runID))
	if errors.Is(err, knowledge.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mcpserver: advance workflow: load bagong review: %w", err)
	}
	if rec.BagongReview == nil || len(rec.BagongReview.BlockingFindings) == 0 {
		return nil
	}
	// blocking_findings are now structured objects; summarize each so the
	// error names the severity, location, and reason of every blocker.
	summaries := make([]string, 0, len(rec.BagongReview.BlockingFindings))
	for _, f := range rec.BagongReview.BlockingFindings {
		summaries = append(summaries, fmt.Sprintf("[%s] %s: %s", f.Severity, f.Location, f.Why))
	}
	return fmt.Errorf(
		"mcpserver: advance workflow: run %q has %d unresolved blocking Bagong finding(s): %s; resolve each via reopen_task (regression in completed work) or report_discovered_task (new/missing scope), then resubmit a clean submit_lane_review with role bagong before completing",
		runID, len(rec.BagongReview.BlockingFindings), strings.Join(summaries, "; "),
	)
}
