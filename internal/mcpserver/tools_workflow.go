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
		if err := a.Workflow.Append(run); err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: create workflow run: %w", err)
		}
		if err := sources.WriteSessionSummary(ctx, a, run); err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: create workflow run: write summary.yaml: %w", err)
		}
		return nil, run, nil
	}
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
// review (§8.4, recorded under recordID(a, "bagong", runID) - see
// SubmitBagongReviewInput) has unresolved blocking_findings (M8's
// acceptance criterion: "delivery cannot be marked complete with unresolved
// blocking findings"). A run with no Bagong review at all is allowed to
// complete - the review step is optional scaffolding like the rest of the
// pipeline (§28's serverInstructions), not a mandatory gate that would break
// every simple run that never used it.
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
	return fmt.Errorf(
		"mcpserver: advance workflow: run %q has %d unresolved blocking Bagong finding(s): %s; resolve each via reopen_task (regression in completed work) or report_discovered_task (new/missing scope), then resubmit a clean submit_bagong_review before completing",
		runID, len(rec.BagongReview.BlockingFindings), strings.Join(rec.BagongReview.BlockingFindings, "; "),
	)
}
