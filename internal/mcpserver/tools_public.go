package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/agent"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func registerPublicTools(server *mcp.Server, a *app.App, reg *toolIndex, agentReg agent.AgentRegistry) {
	addTool(server, reg, &mcp.Tool{Name: "role_list", Description: "List Punakawan's declared roles (id, name, description, version).", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, roleListHandler(agentReg))
	addTool(server, reg, &mcp.Tool{Name: "role_get", Description: "Fetch one role's full declaration: resolved instructions, output schema, tool policy, and execution policy.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, roleGetHandler(agentReg))
	addTool(server, reg, &mcp.Tool{Name: "list_adapter_operations", Description: "List live adapter operations and input schemas.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, listAdapterOperationsHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "call_adapter_operation", Description: "Invoke one declared adapter operation; use its discovered input schema."}, callAdapterOperationHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "upsert_project", Description: "Create or update a project's repository configuration. Also call this (same slug and repository_url, plus a metadata field) whenever you learn a static configuration fact about a registered project's repository - package manager, layout, naming convention, test framework, linters, formatters - so it survives this session; metadata merges field-by-field, it never overwrites what's already recorded."}, upsertProjectHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "list_projects", Description: "Find registered projects without dumping the registry. For normal work, run `git remote get-url origin` and pass it as repository_url. For deliberate multi-project work, pass explicit slugs or a bounded query. One selector is required.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, listProjectsHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "save_workflow", Description: "Create or update a reusable workflow definition."}, saveWorkflowDefinitionHandler(a, reg))
	addTool(server, reg, &mcp.Tool{Name: "get_workflow", Description: "Read one reusable workflow definition.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, getWorkflowHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "list_workflows", Description: "List reusable workflow definitions.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, listWorkflowsHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "invoke_workflow", Description: "Resolve a reusable workflow into a plan and runtime."}, invokeWorkflowDefinitionHandler(a, agentReg))
	addTool(server, reg, &mcp.Tool{Name: "plan_save", Description: "Save an immutable Plan revision. Call this as soon as an implementation plan is produced or approved - before starting execution, and again after any revision - so plan_get can find it later in this or a resumed session."}, planSaveHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "plan_get", Description: "Fetch a Plan revision. Call this at the start of any planning or execution work to check for an already-saved plan before re-deriving one from scratch.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, planGetHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "start_delivery", Description: "Start or resume a delivery from a provider-neutral source (jira | adhoc). Pass projects - the repositories the work lands in and the tasks to open there - or the delivery gets no lanes and cannot run; pass session, or nothing measures its tokens, cost or tool calls. Calling it again for the same source reconciles newly discovered work onto the same delivery instead of starting a second one. Returns the orchestration and execution ids, the captured requirement sources, the lanes it created, and reconciliation.skipped naming anything it could not create."}, startDeliveryHandler(a, agentReg))
	addTool(server, reg, &mcp.Tool{Name: "start_delivery_session", Description: "Start or resume a durable session for a delivery execution."}, startDeliverySessionHandler(a, reg))
	addTool(server, reg, &mcp.Tool{Name: "checkpoint_delivery_session", Description: "Persist a session checkpoint and optional handoff."}, checkpointDeliverySessionHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "ingest_delivery_usage_snapshot", Description: "Record a monotonic cumulative usage snapshot for one delivery session's turn or named subagent."}, ingestDeliveryUsageSnapshotHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "finalize_delivery_session", Description: "Close a delivery session exactly once, applying its final usage snapshot atomically."}, finalizeDeliverySessionHandler(a, reg))
	addTool(server, reg, &mcp.Tool{Name: "report_delivery_usage", Description: "Deprecated: use ingest_delivery_usage_snapshot and finalize_delivery_session instead."}, reportDeliveryUsageHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "report_delivery_progress", Description: "Record a durable delivery progress report."}, reportDeliveryProgressHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "assess_jira_delivery", Description: "Record Jira source clarity and rationale."}, assessJiraDeliveryHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "hydrate_jira_delivery", Description: "Fetch an exact Jira issue through the configured adapter and persist an immutable source snapshot."}, hydrateJiraDeliveryHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "hydrate_github_pull_request", Description: "Fetch a GitHub pull request, diff files, checks, comments, and unresolved review threads for agent review."}, hydrateGitHubPullRequestHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "propose_github_pr_review", Description: "Persist agent review findings and an exact GitHub pull-request review proposal before submission."}, proposeGitHubPRReviewHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "get_github_pr_review", Description: "Read a persisted GitHub pull-request review proposal and its submission status.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, getGitHubPRReviewHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "submit_github_pr_review", Description: "Submit one persisted GitHub pull-request review through the configured adapter."}, submitGitHubPRReviewHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "map_delivery_work_item", Description: "Bind a delivery work item to an exact captured Jira task before worklogging. Takes the execution_id and requirement_source_id start_delivery returned, plus the lane's parent_task_id; log_delivery_work is refused until this is done."}, mapDeliveryWorkItemHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "get_delivery", Description: "Read a delivery's complete current state and next action.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, getDeliveryHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "answer_delivery_question", Description: "Answer one pending delivery clarification."}, answerDeliveryQuestionHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "log_delivery_work", Description: "Record measured work on an exact Jira task and synchronize it when configured. Needs the lane_id from get_delivery, and needs that lane's parent task already bound to the issue with map_delivery_work_item - without the binding this is refused, since time would otherwise be recorded against a task nothing identifies."}, logDeliveryWorkHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "retry_worklog_sync", Description: "Retry Jira synchronization for one existing worklog without recording duplicate time."}, retryWorkLogSyncHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "post_jira_comment", Description: "Post a free-text comment on an exact Jira issue or subtask through the configured adapter."}, postJiraCommentHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "complete_delivery_lane", Description: "Close one lane's work, recording what was verified and whether the lane was accepted or failed. This is what moves a lane out of runnable - without it a lane stays open forever and its six verification dimensions stay pending. Needs the lane_id and expected_revision from get_delivery, and needs that lane's work already recorded with log_delivery_work."}, completeDeliveryLaneHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "cancel_delivery", Description: "Cancel a non-terminal delivery while preserving its audit history."}, cancelDeliveryHandler(a))
	addTool(server, reg, &mcp.Tool{Name: "complete_delivery", Description: "Atomically complete a delivery execution's terminal state."}, completeDeliveryHandler(a))
}

type UpsertProjectInput struct {
	Slug          string `json:"slug"`
	RepositoryURL string `json:"repository_url"`
	DefaultBranch string `json:"default_branch,omitempty"`
	// LocalPath records where this repository is checked out, so a
	// delivery for it can be started from any directory.
	LocalPath string `json:"local_path,omitempty" jsonschema:"absolute path of this repository's checkout on this machine; recorded so a delivery for this project can be started from anywhere"`
	// Metadata is optional and merged field-by-field into whatever is
	// already stored, not replaced wholesale: pass only the facts you
	// actually know, e.g. {"package_manager": "pnpm"}.
	Metadata *protocol.DeliveryProjectMetadata `json:"metadata,omitempty"`
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
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, UpsertProjectOutput{}, err
		}
		project, err := store.UpsertProject(ctx, delivery.NewID(), delivery.NewID(), in.Slug, in.RepositoryURL, strings.TrimSpace(in.DefaultBranch))
		if err != nil {
			return nil, UpsertProjectOutput{}, fmt.Errorf("mcpserver: upsert_project: %w", err)
		}
		if in.Metadata != nil {
			project, err = store.MergeProjectMetadata(ctx, delivery.NewID(), project.Id, *in.Metadata)
			if err != nil {
				return nil, UpsertProjectOutput{}, fmt.Errorf("mcpserver: upsert_project: merge metadata: %w", err)
			}
		}
		if localPath := strings.TrimSpace(in.LocalPath); localPath != "" {
			branch := strings.TrimSpace(in.DefaultBranch)
			if branch == "" && project.DefaultBranch != nil {
				branch = *project.DefaultBranch
			}
			if err := store.RememberProjectCheckout(ctx, delivery.NewID(), project.Id, localPath, "origin", branch); err != nil {
				return nil, UpsertProjectOutput{}, fmt.Errorf("mcpserver: upsert_project: record local path: %w", err)
			}
		}
		return nil, UpsertProjectOutput{Project: *project}, nil
	}
}

const (
	defaultProjectSearchLimit = 10
	maxProjectSearchLimit     = 50
	maxProjectSlugSelection   = 50
)

type ListProjectsInput struct {
	// RepositoryURL is the current checkout's Git origin. It has exact,
	// normalized matching semantics and returns zero or one project.
	RepositoryURL string `json:"repository_url,omitempty"`
	// Slugs deliberately selects one or more known projects for a
	// cross-project delivery.
	Slugs []string `json:"slugs,omitempty"`
	// Query performs a bounded case-insensitive contains search over slug
	// and repository URL when the caller does not know an exact slug.
	Query string `json:"query,omitempty"`
	// Limit applies only to Query; zero uses defaultProjectSearchLimit.
	Limit int `json:"limit,omitempty"`
}

type ListProjectsOutput struct {
	Projects []protocol.DeliveryProject `json:"projects"`
}

func listProjectsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListProjectsInput) (*mcp.CallToolResult, ListProjectsOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListProjectsInput) (*mcp.CallToolResult, ListProjectsOutput, error) {
		in.RepositoryURL = strings.TrimSpace(in.RepositoryURL)
		in.Query = strings.TrimSpace(in.Query)
		slugs := make([]string, 0, len(in.Slugs))
		seenSlugs := make(map[string]struct{}, len(in.Slugs))
		for _, slug := range in.Slugs {
			slug = strings.TrimSpace(slug)
			if slug == "" {
				return nil, ListProjectsOutput{}, fmt.Errorf("mcpserver: list_projects: slugs cannot contain an empty value")
			}
			if _, seen := seenSlugs[slug]; seen {
				continue
			}
			seenSlugs[slug] = struct{}{}
			slugs = append(slugs, slug)
		}
		selectors := 0
		if in.RepositoryURL != "" {
			selectors++
		}
		if len(slugs) > 0 {
			selectors++
		}
		if in.Query != "" {
			selectors++
		}
		if selectors != 1 {
			return nil, ListProjectsOutput{}, fmt.Errorf("mcpserver: list_projects: supply exactly one of repository_url, slugs, or query")
		}
		if len(slugs) > maxProjectSlugSelection {
			return nil, ListProjectsOutput{}, fmt.Errorf("mcpserver: list_projects: at most %d slugs are allowed", maxProjectSlugSelection)
		}
		if in.Query == "" && in.Limit != 0 {
			return nil, ListProjectsOutput{}, fmt.Errorf("mcpserver: list_projects: limit is only valid with query")
		}

		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, ListProjectsOutput{}, err
		}
		var projects []protocol.DeliveryProject
		switch {
		case in.RepositoryURL != "":
			projects, err = store.FindProjectsByRepositoryURL(ctx, in.RepositoryURL)
			if err == nil && len(projects) > 1 {
				return nil, ListProjectsOutput{}, fmt.Errorf("mcpserver: list_projects: repository identity is ambiguous; use slugs")
			}
		case len(slugs) > 0:
			projects, err = store.FindProjectsBySlugs(ctx, slugs)
		default:
			limit := in.Limit
			if limit == 0 {
				limit = defaultProjectSearchLimit
			}
			if limit < 1 || limit > maxProjectSearchLimit {
				return nil, ListProjectsOutput{}, fmt.Errorf("mcpserver: list_projects: query limit must be between 1 and %d", maxProjectSearchLimit)
			}
			projects, err = store.SearchProjects(ctx, in.Query, limit)
		}
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
		store, err := openWorkflowDefinitions(a)
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
		store, err := openWorkflowDefinitions(a)
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
