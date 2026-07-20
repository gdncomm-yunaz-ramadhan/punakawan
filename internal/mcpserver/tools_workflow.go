package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
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
		run := workflow.New(in.RunId, a.Workspace.ID, protocol.WorkflowRunWorkflowName(in.WorkflowName), time.Now().UTC())
		if err := a.Workflow.Append(run); err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: create workflow run: %w", err)
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

		run, err = workflow.Advance(run, protocol.WorkflowRunState(in.NextState), in.Note, time.Now().UTC())
		if err != nil {
			return nil, protocol.WorkflowRun{}, err
		}

		if err := a.Workflow.Append(run); err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: persist workflow advance: %w", err)
		}
		return nil, run, nil
	}
}
