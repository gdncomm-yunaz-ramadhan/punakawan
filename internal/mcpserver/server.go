// Package mcpserver implements Punakawan's own MCP server (§28), exposing
// Semar/Gareng/Petruk/Bagong as prompts and the supporting data operations
// as tools. Punakawan performs no reasoning itself: a connected MCP client
// fetches a role's prompt, reasons over the supplied context with its own
// model, and submits the structured result back through a submit_* tool,
// which this package validates and persists (§28.2).
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
)

// Serve starts Punakawan's MCP server over stdio and blocks until the
// connected client disconnects, per §28.4 ("Exposed as `punakawan mcp
// serve` (stdio transport)").
func Serve(ctx context.Context, a *app.App) error {
	server, err := newServer(a)
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}

// serverInstructions is surfaced to every connecting MCP client as part of
// the initialize handshake (InitializeResult.Instructions - "a hint to the
// model", per the MCP spec). This is the one piece of guidance punakawan can
// hand an agent automatically, regardless of which project repo it is
// running in (unlike a CLAUDE.md, which would have to be copied into every
// consuming project) - so it's the right place for the two things that
// actually tripped up real usage: the expected tool call sequence, and how
// the write-approval gate is meant to be satisfied.
const serverInstructions = `Punakawan never reasons itself (ADR-0016): you, the connected agent, are the reasoning engine. Punakawan validates and persists whatever structured result you submit, and enforces write approvals - it does not call a model on its own.

Two independent mechanisms, don't conflate them:

1. The workflow pipeline (create_workflow_run -> submit_task_graph -> claim_ready_task -> start_task_execution/build_task_context -> submit_*_review/submit_petruk_plan/submit_semar_synthesis -> finish_task_execution -> commit_task, tracked via get_workflow_state/advance_workflow) is for durable, multi-session/multi-person work: decomposing a large requirement, persisting context and plan/review findings so a later session or teammate doesn't start from zero. It is optional scaffolding, not a prerequisite for anything else - it does not gate approvals or adapter writes in any way.

2. External writes (Jira/Confluence edits, comments, transitions, worklogs - via call_adapter_operation or higher-level tools like update_jira_task_progress, sync_jira_subtasks, request_jira_clarification) are approval-gated per run_id, always, regardless of whether a workflow run or task graph exists for that run_id at all. Calling one before it is approved creates a pending approval request and returns an error; it does not perform the write and does not silently no-op without telling you why. Approval is granted by a human via the "punakawan approvals list/approve/deny" CLI (accepts multiple ids in one call), never another MCP tool - this is deliberate, so the same agent requesting a write cannot also approve it. For a single one-off write with no workflow run at all: call the operation directly (this records the pending request), have the human approve it, then retry the same call - the workflow pipeline above is not required first.`

// newServer builds the *mcp.Server with every prompt and tool registered,
// independent of which transport it will run over. Split out from Serve so
// tests can connect to it via an in-memory transport instead of stdio.
func newServer(a *app.App) (*mcp.Server, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "punakawan", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})

	if err := registerPrompts(server); err != nil {
		return nil, err
	}
	registerTools(server, a)

	return server, nil
}
