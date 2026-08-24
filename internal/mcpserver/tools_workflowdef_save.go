package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

type SaveWorkflowDefinitionInput struct {
	Definition workflowdef.Definition `json:"definition"`
}

type SaveWorkflowDefinitionOutput struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Action   string `json:"action"`
}

func saveWorkflowDefinitionHandler(a *app.App, reg *toolIndex) func(context.Context, *mcp.CallToolRequest, SaveWorkflowDefinitionInput) (*mcp.CallToolResult, SaveWorkflowDefinitionOutput, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in SaveWorkflowDefinitionInput) (*mcp.CallToolResult, SaveWorkflowDefinitionOutput, error) {
		definition := in.Definition
		if definition.Version == "" {
			definition.Version = workflowdef.SchemaVersion
		}
		if err := workflowdef.Validate(definition, workflowdef.NewCapabilitySet(reg.Names(), nil)); err != nil {
			return nil, SaveWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: save_workflow: %w", err)
		}
		store, err := workflowdef.Open(a.Workspace.Root)
		if err != nil {
			return nil, SaveWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: save_workflow: %w", err)
		}
		action := "updated"
		if _, err := store.Get(definition.ID); err != nil {
			if !errors.Is(err, workflowdef.ErrNotFound) {
				return nil, SaveWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: save_workflow: %w", err)
			}
			action = "created"
		}
		saved, err := store.Save(definition)
		if err != nil {
			return nil, SaveWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: save_workflow: %w", err)
		}
		return nil, SaveWorkflowDefinitionOutput{ID: saved.ID, Revision: saved.Revision, Action: action}, nil
	}
}
