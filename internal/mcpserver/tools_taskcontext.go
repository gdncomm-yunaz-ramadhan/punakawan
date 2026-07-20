package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/taskcontext"
)

// BuildTaskContextInput is build_task_context's input, per §11.2.
type BuildTaskContextInput struct {
	TaskId                        string   `json:"task_id"`
	RequirementId                 string   `json:"requirement_id"`
	TaskScope                     string   `json:"task_scope,omitempty"`
	TaskAcceptanceCriteria        []string `json:"task_acceptance_criteria,omitempty"`
	TaskDefinitionOfDone          string   `json:"task_definition_of_done,omitempty"`
	TaskExpectedFilesOrComponents []string `json:"task_expected_files_or_components,omitempty"`
	AffectedSymbolsAndFiles       []string `json:"affected_symbols_and_files,omitempty"`
	RequiredTests                 []string `json:"required_tests,omitempty"`
	KnownConstraints              []string `json:"known_constraints,omitempty"`
	PreviousTaskOutputs           []string `json:"previous_task_outputs,omitempty"`
	RunId                         string   `json:"run_id" jsonschema:"the run this task belongs to, for the task.yaml evidence bundle"`
}

func buildTaskContextHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, BuildTaskContextInput) (*mcp.CallToolResult, taskcontext.Context, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in BuildTaskContextInput) (*mcp.CallToolResult, taskcontext.Context, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, taskcontext.Context{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		built, err := taskcontext.Build(ctx, store, taskcontext.BuildInput{
			TaskID:                        in.TaskId,
			RequirementID:                 in.RequirementId,
			TaskScope:                     in.TaskScope,
			TaskAcceptanceCriteria:        in.TaskAcceptanceCriteria,
			TaskDefinitionOfDone:          in.TaskDefinitionOfDone,
			TaskExpectedFilesOrComponents: in.TaskExpectedFilesOrComponents,
			AffectedSymbolsAndFiles:       in.AffectedSymbolsAndFiles,
			RequiredTests:                 in.RequiredTests,
			KnownConstraints:              in.KnownConstraints,
			PreviousTaskOutputs:           in.PreviousTaskOutputs,
		})
		if err != nil {
			return nil, taskcontext.Context{}, fmt.Errorf("mcpserver: build task context: %w", err)
		}

		bundle, err := newEvidenceBundle(a, in.RunId, in.TaskId)
		if err != nil {
			return nil, taskcontext.Context{}, err
		}
		if err := taskcontext.WriteToBundle(built, bundle); err != nil {
			return nil, taskcontext.Context{}, fmt.Errorf("mcpserver: write task.yaml: %w", err)
		}

		return nil, built, nil
	}
}
