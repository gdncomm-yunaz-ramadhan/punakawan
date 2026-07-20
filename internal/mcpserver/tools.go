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
}
