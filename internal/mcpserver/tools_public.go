package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func registerPublicTools(server *mcp.Server, a *app.App, reg *toolIndex) {
	addTool(server, reg, &mcp.Tool{Name: "upsert_project", Description: "Create or update a project's repository configuration."}, upsertProjectHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "list_projects", Description: "List registered projects and concise repository metadata."}, listProjectsHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "save_workflow", Description: "Create or update a reusable workflow definition."}, saveWorkflowDefinitionHandler(a, reg))
	addTool(server, reg, &mcp.Tool{Name: "get_workflow", Description: "Read one reusable workflow definition."}, getWorkflowHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "list_workflows", Description: "List reusable workflow definitions."}, listWorkflowsHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "invoke_workflow", Description: "Resolve a reusable workflow into a plan and runtime."}, invokeWorkflowDefinitionHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "plan_save", Description: "Save an immutable Plan revision."}, planSaveHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "plan_get", Description: "Fetch a Plan revision."}, planGetHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "start_delivery", Description: "Start a delivery from requirement references and project routing."}, startDeliveryHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "get_delivery", Description: "Read a delivery's complete current state and next action."}, getDeliveryHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "answer_delivery_question", Description: "Answer one pending delivery clarification."}, answerDeliveryQuestionHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "cancel_delivery", Description: "Cancel a non-terminal delivery while preserving its audit history."}, cancelDeliveryHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "approve_project_delivery", Description: "Approve or reject a project's pending delivery gate."}, approveProjectDeliveryHandler(a))
}

type UpsertProjectInput struct {
	Slug          string `json:"slug"`
	RepositoryURL string `json:"repository_url"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

type UpsertProjectOutput struct {
	Project protocol.DeliveryProject `json:"project"`
}

func upsertProjectHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, UpsertProjectInput) (*mcp.CallToolResult, UpsertProjectOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in UpsertProjectInput) (*mcp.CallToolResult, UpsertProjectOutput, error) {
		in.Slug = strings.TrimSpace(in.Slug)
		in.RepositoryURL = strings.TrimSpace(in.RepositoryURL)
		if in.Slug == "" || in.RepositoryURL == "" {
			return nil, UpsertProjectOutput{}, fmt.Errorf("mcpserver: upsert_project: slug and repository_url are required")
		}
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, UpsertProjectOutput{}, err
		}
		project, err := store.UpsertProject(ctx, delivery.NewID(), delivery.NewID(), in.Slug, in.RepositoryURL, strings.TrimSpace(in.DefaultBranch))
		if err != nil {
			return nil, UpsertProjectOutput{}, fmt.Errorf("mcpserver: upsert_project: %w", err)
		}
		return nil, UpsertProjectOutput{Project: *project}, nil
	}
}

type ListProjectsOutput struct {
	Projects []protocol.DeliveryProject `json:"projects"`
}

func listProjectsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, ListProjectsOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListProjectsOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, ListProjectsOutput{}, err
		}
		projects, err := store.ListProjects(ctx)
		if err != nil {
			return nil, ListProjectsOutput{}, fmt.Errorf("mcpserver: list_projects: %w", err)
		}
		return nil, ListProjectsOutput{Projects: projects}, nil
	}
}

type GetWorkflowInput struct {
	ID string `json:"id"`
}

type GetWorkflowOutput struct {
	Workflow workflowdef.Definition `json:"workflow"`
}

func getWorkflowHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, GetWorkflowInput) (*mcp.CallToolResult, GetWorkflowOutput, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in GetWorkflowInput) (*mcp.CallToolResult, GetWorkflowOutput, error) {
		store, err := workflowdef.Open(a.Workspace.Root)
		if err != nil {
			return nil, GetWorkflowOutput{}, fmt.Errorf("mcpserver: get_workflow: %w", err)
		}
		workflow, err := store.Get(in.ID)
		if err != nil {
			return nil, GetWorkflowOutput{}, fmt.Errorf("mcpserver: get_workflow: %w", err)
		}
		return nil, GetWorkflowOutput{Workflow: workflow}, nil
	}
}

type ListWorkflowsOutput struct {
	Workflows []workflowdef.Definition `json:"workflows"`
}

func listWorkflowsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, ListWorkflowsOutput, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListWorkflowsOutput, error) {
		store, err := workflowdef.Open(a.Workspace.Root)
		if err != nil {
			return nil, ListWorkflowsOutput{}, fmt.Errorf("mcpserver: list_workflows: %w", err)
		}
		workflows, err := store.List()
		if err != nil {
			return nil, ListWorkflowsOutput{}, fmt.Errorf("mcpserver: list_workflows: %w", err)
		}
		return nil, ListWorkflowsOutput{Workflows: workflows}, nil
	}
}
