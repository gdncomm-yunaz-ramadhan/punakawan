package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
)

// registerTools adds the data-operation tools defined in §28.4, plus
// create_workflow_run: §28.4 lists get_workflow_state/advance_workflow but
// not a way to start a run in the first place, and the server cannot
// function without one, so this is a necessary addition beyond the plan's
// literal tool list rather than an unstated one.
func registerTools(server *mcp.Server, a *app.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "build_context_dossier",
		Description: "Assemble the §9.1 context dossier from workspace, git, and durable knowledge state. No reasoning is performed.",
	}, buildContextDossierHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_gareng_review",
		Description: "Validate and persist a Gareng feasibility/risk review (§8.2) as durable knowledge.",
	}, submitGarengReviewHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_petruk_plan",
		Description: "Validate and persist a Petruk implementation-planning output (§8.3) as durable knowledge.",
	}, submitPetrukPlanHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_semar_synthesis",
		Description: "Validate and persist Semar's consolidated clarification (§8.1/§9.2) or final plan (§9.3) as durable knowledge. Exactly one of synthesis or final_plan must be set.",
	}, submitSemarSynthesisHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_bagong_review",
		Description: "Validate and persist a Bagong independent final review (§8.4) as durable knowledge.",
	}, submitBagongReviewHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_workflow_run",
		Description: "Start a new workflow run in state \"created\" (§18.1).",
	}, createWorkflowRunHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_workflow_state",
		Description: "Read a workflow run's current state and checkpoint history (§18.1).",
	}, getWorkflowStateHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "advance_workflow",
		Description: "Transition a workflow run to a new state, appending a checkpoint (§18.1).",
	}, advanceWorkflowHandler(a))

	// Milestone 6: Plan-to-Beads and Petruk execution (§10, §11).
	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_task_graph",
		Description: "Batch-create TaskContracts and wire their dependency edges into Beads (§10.1-§10.4). The calling role does the decomposition; this tool only creates and wires the result.",
	}, submitTaskGraphHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_ready_tasks",
		Description: "List Beads issues with no active blockers (§9's 'Petruk executes ready task'). Read-only.",
	}, listReadyTasksHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "claim_ready_task",
		Description: "Atomically claim the first ready Beads issue matching the filters (§11.3's 'claim task' step). Mutates issue state.",
	}, claimReadyTaskHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "build_task_context",
		Description: "Assemble the fresh, bounded per-task execution context (§11.2) and write it as this task's task.yaml evidence (§17.2). Read-only against the knowledge store.",
	}, buildTaskContextHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "start_task_execution",
		Description: "Create this task's isolated worktree and open its evidence bundle/journal (§11.1 steps 1-4). Requires a prior approved worktree-creation request.",
	}, startTaskExecutionHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "finish_task_execution",
		Description: "Record this task's final status and remove its isolated worktree (§11.1 step 10).",
	}, finishTaskExecutionHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_file",
		Description: "Write one file within a task's worktree, policy-checked and confined to the worktree root (§15.4, §3.1). Use instead of writing to disk directly.",
	}, writeFileHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "bulk_create_files",
		Description: "Create several files within a task's worktree in one call, with the same checks as write_file, best-effort per file.",
	}, bulkCreateFilesHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_diff",
		Description: "Stage and check a task's pending changes against policy and a heuristic secret scan (§15.4), writing diff.patch evidence (§17.2). Must pass before commit_task.",
	}, checkDiffHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_tests",
		Description: "Run caller-specified compile/test commands through the tool supervisor and record a tests.json evidence report (§11.3, §17.2).",
	}, runTestsHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_openapi_compatibility",
		Description: "Diff a base and head OpenAPI spec, classify breaking changes, and record api-diff.json evidence (§13.4, §17.2).",
	}, checkOpenAPICompatibilityHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "commit_task",
		Description: "Stage and commit a task's pending changes, refusing to do so unless a prior check_diff passed and the worktree is on a task branch (§15.4).",
	}, commitTaskHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "report_discovered_task",
		Description: "Record newly discovered work found mid-execution as a discovered-from task, labeled for Semar's review (§10.4's discovery rule).",
	}, reportDiscoveredTaskHandler(a))
}
