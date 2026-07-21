// Package mcpserver implements Punakawan's own MCP server (§28), exposing
// Semar/Gareng/Petruk/Bagong as prompts and the supporting data operations
// as tools. Punakawan performs no reasoning itself: a connected MCP client
// fetches a role's prompt, reasons over the supplied context with its own
// model, and submits the structured result back through a submit_* tool,
// which this package validates and persists (§28.2).
package mcpserver

import (
	"context"
	"encoding/json"

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

2. External writes (Jira/Confluence edits, comments, attachments, transitions, worklogs - via call_adapter_operation or higher-level tools like update_jira_task_progress, sync_jira_subtasks, request_jira_clarification, submit_jira_assessment) are approval-gated per run_id, always, regardless of whether a workflow run or task graph exists for that run_id at all. One human approval covers every approval-required adapter operation during the run. Punakawan first asks the connected MCP client to elicit Approve/Deny inline. If the client does not support form elicitation, the write remains pending: show the error's Approve and Deny choices to the human, never choose for them, and only after their explicit response call respond_to_adapter_approval and retry an approved operation. The CLI commands punakawan approvals approve and punakawan approvals deny remain alternatives. The workflow pipeline above is not required before a one-off write.

When asked to work a Jira ticket end to end, do this before writing any code: read the ticket, assess what already exists in the repo versus what the requirement needs, and call submit_jira_assessment with your findings, any open questions that need a stakeholder decision (flag the important ones), and one task per unit of work with a detailed plan and both an ai_hours and human_hours estimate. ai_hours becomes the Jira estimate on that subtask; human_hours and the resulting time saved are narrative only.`

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
	server.AddReceivingMiddleware(compactStructuredToolResults)

	return server, nil
}

// compactStructuredToolResults removes the Go SDK's automatic full JSON copy
// from content when the same value is already present in structuredContent.
// Modern MCP clients (including Codex and Claude) consume structuredContent;
// retaining a two-word content marker keeps the response legible to older
// clients without charging the model context twice for every result.
func compactStructuredToolResults(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		if err != nil || method != "tools/call" {
			return result, err
		}
		toolResult, ok := result.(*mcp.CallToolResult)
		if !ok || toolResult.IsError || toolResult.StructuredContent == nil || len(toolResult.Content) != 1 {
			return result, nil
		}
		text, ok := toolResult.Content[0].(*mcp.TextContent)
		if !ok {
			return result, nil
		}
		structured, marshalErr := json.Marshal(toolResult.StructuredContent)
		if marshalErr == nil && text.Text == string(structured) {
			text.Text = "Structured result."
		}
		return result, nil
	}
}
