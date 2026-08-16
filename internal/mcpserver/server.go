// Package mcpserver implements Punakawan's own MCP server (§28), exposing
// Semar/Gareng/Petruk/Bagong as prompts and the supporting data operations
// as tools. Punakawan performs no reasoning itself: a connected MCP client
// fetches a role's prompt, reasons over the supplied context with its own
// model, and submits the structured result back through a submit_* tool,
// which this package validates and persists (§28.2).
package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/capability"
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
const serverInstructionsBody = `Punakawan never reasons itself (ADR-0016): you, the connected agent, are the reasoning engine. Punakawan validates and persists whatever structured result you submit, and enforces write approvals - it does not call a model on its own.

Only a small default facade is visible at first (delivery, project/lane setup, workflow invocation, and find_tool). The rest of the surface - Jira, knowledge, git/PR, role review, adapters, and more - exists and is fully capable, just not listed yet: call find_tool with a keyword (or select:name1,name2 for exact names) to make any of it callable immediately. If a tool you expect isn't in your list, that's why - search for it rather than assuming it doesn't exist.

Guiding principle: grounded truth over confident performance. Keep the work grounded, honest, practical, cautious, and verifiable. When you act as a role (Semar, Gareng, Petruk, Bagong) the role prompt carries the shared communication rules - lead with the conclusion, reference evidence by id/file/symbol/artifact instead of copying it, include only what changes a decision or verification, distinguish fact from inference, and keep every result concise. They are stated once there; this instruction does not repeat them.

Two independent mechanisms, don't conflate them:

1. The workflow pipeline (create_workflow_run -> submit_task_graph -> claim_ready_task -> start_task_execution/build_task_context -> submit_lane_bagong_review/submit_final_plan -> finish_task_execution -> commit_task, tracked via get_workflow_state/advance_workflow) is for durable, multi-session/multi-person work: decomposing a large requirement, persisting context and plan/review findings so a later session or teammate doesn't start from zero. It is optional scaffolding, not a prerequisite for anything else - it does not gate approvals or adapter writes in any way.

   Context loop (agent-context plan): before substantial project work, call prepare_work_context once - it resolves the workflow (by workflow_id or an exact capability/intent selector; ad hoc when neither matches), returns the bounded, deterministic context (required metadata and selected knowledge, each with a reason and content hash; lifecycle-unsafe knowledge is filtered out), and returns a run_id. If it reports missing context the run is in awaiting-clarification: resolve the gaps and re-call it. For a definition-backed run, use get_next_workflow_step to see ready/blocked steps (a capability the workflow does not allow shows as blocked) and complete_workflow_step to finish each with evidence or a deviation reason. Reuse an accepted workflow when there is one clear match; otherwise proceed ad hoc and record the actual path - do not invent a canonical workflow. Before completing a context-aware run, call record_work_outcome (status, summary, evidence, deviations, and any reusable observations classified for a later proposal): a context-aware run cannot advance to completed without a recorded outcome, and a definition-backed one also needs all steps completed. An observation is a traceable input to a reviewed proposal, never canonical until a human accepts it.

2. External writes (Jira/Confluence edits, comments, attachments, transitions, worklogs - via call_adapter_operation or higher-level tools like update_jira_task_progress, sync_jira_subtasks, request_jira_clarification, submit_jira_assessment) are approval-gated per run_id, always, regardless of whether a workflow run or task graph exists for that run_id at all. One human approval covers every approval-required adapter operation during the run. Punakawan first asks the connected MCP client to elicit Approve/Deny inline. If the client does not support form elicitation, the write remains pending: show the error's Approve and Deny choices to the human, never choose for them, and only after their explicit response call respond_to_adapter_approval and retry an approved operation. The CLI commands punakawan approvals approve and punakawan approvals deny remain alternatives. The workflow pipeline above is not required before a one-off write. Approvals are durable per run_id, so if a session ends while one is pending, do not restart the work from scratch: a resumed or new session can call list_pending_approvals (read-only, optionally filtered by run_id) to see what is still pending and continue - surface Approve/Deny to the user, resolve, then retry. This is also the safe way to poll/loop while waiting rather than blindly re-issuing the write.

When asked to work a Jira ticket end to end, do this before writing any code: read the ticket, assess what already exists in the repo versus what the requirement needs, and call submit_jira_assessment with your findings, any open questions that need a stakeholder decision (flag the important ones), and one task per unit of work with a detailed plan and both an ai_hours and human_hours estimate. ai_hours becomes the Jira estimate on that subtask; human_hours and the resulting time saved are narrative only.

Bagong's independent review (§8.4): build your own context rather than trusting Petruk's summary - call build_task_context fresh (it re-pulls the parent requirement, not Petruk's interpretation of it) and read the task's own evidence bundle files directly (diff.patch, tests.json, api-diff.json under .punakawan/evidence/<run>/<task>/). Milestone 7 (Playwright) has not shipped yet, so there is no E2E trace evidence to review - say so explicitly in test_gaps instead of inventing E2E findings or blocking on missing infra. Put a finding in blocking_findings, not the plain findings array, only when it means the delivered work does not satisfy the requirement's acceptance criteria, introduces a regression, or is a security issue. For each blocking finding, take action: call reopen_task if it is a regression in already-completed work, or report_discovered_task if it is new/missing scope - then resubmit a clean submit_lane_bagong_review once resolved. advance_workflow to completed is refused while the run's latest Bagong review still has unresolved blocking_findings.

Token efficiency (RTK): model context is the scarce resource. Run your own shell and dev commands - git, tests, builds, greps, package managers - through rtk whenever it is available (for example "rtk git status", "rtk go test ./..."), so their output costs 60-90% fewer tokens. Punakawan routes the commands it runs itself (such as run_tests) through rtk when it is present on PATH, and you should do the same for the commands you run; fall back to the raw command only when rtk is not installed.`

// serverInstructionsRevision identifies serverInstructionsBody's exact
// content: a client reconnecting after a punakawan upgrade can compare
// this against what it saw last session to tell "the guidance changed,
// re-read it" from "same daemon, nothing to do" - without any dedicated
// tool or resource, since InitializeResult.Instructions (what this
// becomes part of) is already fetched exactly once per session by every
// MCP client, satisfying "fetched once" as a direct consequence of the
// protocol's own handshake.
var serverInstructionsRevision = func() string {
	sum := sha256.Sum256([]byte(serverInstructionsBody))
	return hex.EncodeToString(sum[:8])
}()

var serverInstructions = serverInstructionsBody + "\n\nInstructions revision: " + serverInstructionsRevision

// facadeTools is the small, always-visible default discovery surface: the
// delivery facade a normal caller needs, plus find_tool itself. Every other
// registered tool (the ~90-tool worker-side surface: Jira, knowledge, git/
// PR, role review, adapters, ...) still registers exactly as before, so
// capability.Registry/workflowdef validation and CallTool-by-name (once
// found) are unaffected - only its default tools/list visibility changes.
// This is the AC2 gap an earlier design review had reinterpreted narrower
// rather than actually built: reduce default discovery, not remove
// capability.
var facadeTools = map[string]bool{
	"start_delivery":             true,
	"get_delivery":               true,
	"resume_delivery":            true,
	"answer_delivery_question":   true,
	"approve_project_delivery":   true,
	"cancel_delivery":            true,
	"invoke_workflow_definition": true,
	"register_project":           true,
	"create_parent_task":         true,
	"create_lane":                true,
	"add_dependency_edge":        true,
	"find_tool":                  true,
}

// newServer builds the *mcp.Server with every prompt and tool registered,
// hidden down to the default facade (see facadeTools/find_tool), independent
// of which transport it will run over. Split out from Serve so tests can
// connect to it via an in-memory transport instead of stdio.
func newServer(a *app.App) (*mcp.Server, error) {
	server, idx, err := assembleServer(a)
	if err != nil {
		return nil, err
	}
	idx.hideAllExcept(server, facadeTools)
	return server, nil
}

// assembleServer builds the *mcp.Server with every prompt and tool
// registered and live - the shared construction newServer hides down to the
// default facade afterward, and tests use directly (server_test.go's
// connect) to exercise tool behavior without the visibility feature itself
// getting in the way of every other test in this package.
func assembleServer(a *app.App) (*mcp.Server, *toolIndex, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "punakawan", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})

	if err := registerPrompts(server, a); err != nil {
		return nil, nil, err
	}
	idx := newToolIndex()
	registerTools(server, a, idx)
	registerToolFinder(server, idx)

	server.AddReceivingMiddleware(compactStructuredToolResults)
	server.AddReceivingMiddleware(sanitizeToolListSchemas)

	return server, idx, nil
}

// CapabilityRegistry enumerates the capabilities the MCP server exposes by
// running tool registration against a throwaway server purely to record the
// names. This is the single source of truth the panel's workflow validation
// consults (agent-context plan §4.3): because both the live server and this
// enumeration go through the exact same registerTools statements, the set a
// workflow definition is validated against can never drift from the set the
// server actually registers. (The old hand-maintained mirror had already
// drifted — 46 listed names versus ~70 registered tools.) This always
// returns every capability regardless of find_tool's live/hidden state -
// that's a discovery-surface concern, not a validity one.
//
// Registration only stores tool metadata and handlers; it never runs a
// handler or opens a transport, so this is a cheap one-time call at startup.
func CapabilityRegistry(a *app.App) *capability.Registry {
	idx := newToolIndex()
	server := mcp.NewServer(&mcp.Implementation{Name: "punakawan", Version: "0.1.0"}, nil)
	registerTools(server, a, idx)
	return idx.Registry
}

// sanitizeToolListSchemas rewrites every bare-boolean JSON-Schema subschema
// (jsonschema-go's spec-legal shorthand for "matches anything", produced by
// any Go `any`/`interface{}` field with no jsonschema tag - see protocol
// types generated from *.schema.json, and any hand-written tool struct with
// the same gap) into an equivalent object-shaped schema before a tools/list
// response leaves the server. Claude Code's MCP client rejects a bare
// boolean wherever an object is expected (observed as "Invalid input (at
// tools.N.outputSchema.properties.value)"); because that single occurrence
// fails validation for the whole tools/list array, it silently hides every
// tool this server exposes, not just the offending one (punokawan bug: MCP
// tools never appeared in ToolSearch despite a byte-for-byte correct wire
// response). Fixing this here, once, is more durable than annotating every
// `any` field across a generated file that regenerates and would drop
// hand-added struct tags anyway.
func sanitizeToolListSchemas(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		if err != nil || method != "tools/list" {
			return result, err
		}
		listResult, ok := result.(*mcp.ListToolsResult)
		if !ok {
			return result, nil
		}
		for _, tool := range listResult.Tools {
			if s, ok := tool.InputSchema.(*jsonschema.Schema); ok {
				sanitizeAnySchema(s)
			}
			if s, ok := tool.OutputSchema.(*jsonschema.Schema); ok {
				sanitizeAnySchema(s)
			}
		}
		return result, nil
	}
}

// sanitizeAnySchema fixes s's children in place (s itself is always
// object-typed here: it is a tool's root input/output schema, which the Go
// SDK forces to type "object"). See sanitizeToolListSchemas for why.
func sanitizeAnySchema(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	visit := func(c *jsonschema.Schema) {
		if c == nil {
			return
		}
		if b, err := json.Marshal(c); err == nil && string(b) == "true" {
			c.Description = "any JSON value"
		}
		sanitizeAnySchema(c)
	}
	visit(s.Items)
	for _, c := range s.PrefixItems {
		visit(c)
	}
	visit(s.AdditionalItems)
	visit(s.Contains)
	visit(s.UnevaluatedItems)
	for _, c := range s.Properties {
		visit(c)
	}
	for _, c := range s.PatternProperties {
		visit(c)
	}
	visit(s.AdditionalProperties)
	visit(s.PropertyNames)
	visit(s.UnevaluatedProperties)
	for _, c := range s.AllOf {
		visit(c)
	}
	for _, c := range s.AnyOf {
		visit(c)
	}
	for _, c := range s.OneOf {
		visit(c)
	}
	visit(s.Not)
	visit(s.If)
	visit(s.Then)
	visit(s.Else)
	for _, c := range s.DependentSchemas {
		visit(c)
	}
	visit(s.ContentSchema)
	for _, c := range s.Defs {
		visit(c)
	}
	for _, c := range s.Definitions {
		visit(c)
	}
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
