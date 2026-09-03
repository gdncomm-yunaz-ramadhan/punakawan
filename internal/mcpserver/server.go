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
	"fmt"
	"net/http"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/agent"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/capability"
)

// Serve starts Punakawan's MCP server over stdio and blocks until the
// connected client disconnects, per §28.4 ("Exposed as `punakawan mcp
// serve` (stdio transport)"). This remains the default: a harness that
// spawns punakawan as a local subprocess (Codex, Claude Code) needs
// nothing else.
func Serve(ctx context.Context, a *app.App) error {
	server, err := newServer(a)
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}

// ServeHTTP starts the same public server over Streamable HTTP (the
// MCP-spec-recommended network transport, superseding plain SSE) bound to
// addr, and blocks until ctx is cancelled. This is the harness-reachability
// gap stdio alone leaves: any client that isn't spawning punakawan as a
// local subprocess - one that only speaks to a network-reachable MCP
// endpoint - has no way to connect otherwise, regardless of which model
// provider it is.
//
// This adds a real network listener with no authentication layer of its
// own in this slice - callers binding addr to anything beyond loopback are
// responsible for putting a reverse proxy or auth layer in front of it;
// see the punakawan mcp serve --http flag's own help text.
func ServeHTTP(ctx context.Context, a *app.App, addr string) error {
	server, err := newServer(a)
	if err != nil {
		return err
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	httpServer := &http.Server{Addr: addr, Handler: handler}

	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()

	select {
	case <-ctx.Done():
		return httpServer.Shutdown(context.Background())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("mcpserver: serve http: %w", err)
		}
		return nil
	}
}

// serverInstructions is surfaced to every connecting MCP client as part of
// the initialize handshake (InitializeResult.Instructions - "a hint to the
// model", per the MCP spec). This is the one piece of guidance punakawan can
// hand an agent automatically, regardless of which project repo it is
// running in (unlike a CLAUDE.md, which would have to be copied into every
// consuming project).
//
// It therefore leads with the call order and the prerequisites, which is
// what actually tripped up real usage: an agent that knows every tool by
// name but not that log_delivery_work needs a lane id from get_delivery
// and a prior map_delivery_work_item will call them in an order that
// cannot work, conclude the tooling is broken, and fall back to writing
// Jira comments by hand. Naming a tool that does not exist has the same
// effect, so every name below is checked against the registered surface
// by TestServerInstructionsOnlyNameRealTools.
const serverInstructionsBody = `Punakawan is a focused multi-project delivery orchestrator. It does not reason itself; the connected agent remains the reasoning engine.

Delivery call order. Follow it in this sequence - each step depends on ids the previous one returns:
1. plan_get to check for an already-saved plan before deriving one; plan_save as soon as a plan exists or changes.
2. start_delivery with source (jira needs tenant and key, plus your own clarity - clear or needs_clarification - and, when it is unclear, the rationale that becomes the question asked on the issue; or use adhoc), a projects array naming each repository and its tasks, the plan this delivery executes - either plan with its content, or the plan id and revision of one already saved - and a session naming the participant and the worktree path. Size both to the work: a one-line objective is a plan, and a trivial task needs a word of clarity, not an assessment. Neither is refused when missing - reconciliation.warnings says what the delivery went without, and a missing plan stays visible as a gap at step 7 - so nothing blocks the work while you decide. Passing plan again on a later call for the same issue saves the next revision and moves the delivery onto it, so a plan that changes stays the delivery's plan. Projects are what create lanes; a session is what makes usage measurable. Passing neither leaves a delivery that can neither run nor be measured. The response carries orchestration_id, execution_id, requirement_sources, the lanes just created, and session.telemetry_session_id; reconciliation.skipped names anything that could not be created and why, reconciliation.warnings names what it went ahead without, reconciliation.checkouts names the directory each project resolved to and reconciliation.worktrees the lane worktrees cut in it, and reconciliation.uncovered_requirements names captured requirements no task covers - open work for them, or expect them reported as a gap at step 7. Calling it again for the same source reconciles newly discovered work onto the same delivery rather than starting a second one.
3. map_delivery_work_item, once per lane, binding execution_id plus the lane's parent_task_id and a requirement_source_id to the exact Jira issue or subtask.
4. log_delivery_work when work on that task is done, with the lane_id from get_delivery and a measured interval. It requires the mapping from step 3. Retry an unsynced interval with retry_worklog_sync rather than recording it again.
5. complete_delivery_lane once that lane's work is finished, reporting what you verified and whether the lane was accepted or failed. This is the only thing that moves a lane out of runnable; skip it and the lane stays open forever with all six verification dimensions pending. Failing a lane is a real outcome, not an error.
6. ingest_delivery_usage_snapshot during the session and finalize_delivery_session at its end, both taking the telemetry_session_id from step 2, and reporting provider-observed usage with the current unit price and price source whenever they can be obtained; never ask humans to maintain price tables.
7. complete_delivery, or cancel_delivery if the work is abandoned. It is refused while the delivery still has gaps - open lanes, unreported verification, uncovered requirements, no plan, an unanswered clarity question, unsynced worklogs, open sessions, unpriceable usage. Close them; pass acknowledge_gaps only for a gap you genuinely cannot close, which records it as waived rather than hiding it.

get_delivery reads the current state, its next_action, its readiness (the same gaps step 7 checks), and the ids the steps above need. invoke_workflow starts a delivery from a saved workflow definition instead of step 2.

Optional, and worth doing rather than assumed: assess_jira_delivery records a fresh judgement of the source after the first one at step 2 - after an answer arrives, or after the issue is edited - report_delivery_progress records a durable progress note, and checkpoint_delivery_session records a resumable summary and handoff. Nothing forces them, so a delivery that reports none simply made that choice.

To assess a Jira issue: resolve it, hydrate its parent and every subtask, reason over visible source, then record clarity and rationale with assess_jira_delivery. Propose parent Fibonacci story points from total subtask complexity, and lower agent-assisted original estimates per subtask from expected execution time.

For provider access beyond these tools, use list_adapter_operations to discover live operation descriptions and input schemas, then call_adapter_operation with an exact declared operation. Runtime mechanics stay delegated to connected adapters.

Execute complete, authorized delivery work without asking for confirmation. Ask the user only when a required input is missing or contradictory, or when a material decision has multiple defensible outcomes. In those cases return needs_input with one precise question and, for a decision, finite options with impacts. Do not create approval requests or a pending-question queue of your own. Starting a delivery with clarity needs_clarification is different: punakawan itself records that one question, asks it on the issue, and holds the delivery until answer_delivery_question answers it. So is the first delivery in a project, which asks where its lanes should be worked - a worktree punakawan cuts per lane, or the checkout itself - and creates nothing until answer_delivery_question says which. That answer is remembered per project and never asked again.`

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

// newServer builds the focused public MCP server. Split out from Serve so
// tests can connect through an in-memory transport instead of stdio.
func newServer(a *app.App) (*mcp.Server, error) {
	server, _, err := assembleServer(a)
	return server, err
}

// assembleServer registers the public tools and prompts shared by production
// and in-memory tests.
func assembleServer(a *app.App) (*mcp.Server, *toolIndex, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "punakawan", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})

	if err := registerPrompts(server, a); err != nil {
		return nil, nil, err
	}
	agentReg, err := agent.NewRegistry()
	if err != nil {
		return nil, nil, fmt.Errorf("mcpserver: load agent role registry: %w", err)
	}
	idx := newToolIndex()
	idx.agents = agentReg
	registerPublicTools(server, a, idx, agentReg)

	// Validate every role manifest's output_schema and tool policy against
	// the real schema/capability surfaces now that registerPublicTools has
	// populated idx with every registered tool name - a manifest
	// referencing a tool or output schema that doesn't exist is a startup
	// error, not something to discover at request time.
	schemaChecker, err := agent.NewKnowledgeSchemaChecker()
	if err != nil {
		return nil, nil, fmt.Errorf("mcpserver: load knowledge schema checker: %w", err)
	}
	if err := agent.Validate(agentReg.List(), schemaChecker, idx); err != nil {
		return nil, nil, fmt.Errorf("mcpserver: validate agent roles: %w", err)
	}

	server.AddReceivingMiddleware(sanitizeToolListSchemas)

	return server, idx, nil
}

// CapabilityRegistry enumerates the public capabilities by running the same
// registration used by the live server against a throwaway server.
//
// Registration only stores tool metadata and handlers; it never runs a
// handler or opens a transport, so this is a cheap one-time call at startup.
func CapabilityRegistry(a *app.App) *capability.Registry {
	idx := newToolIndex()
	server := mcp.NewServer(&mcp.Implementation{Name: "punakawan", Version: "0.1.0"}, nil)
	agentReg, err := agent.NewRegistry()
	if err != nil {
		// The 4 role manifests are embedded in the binary and validated by
		// internal/agent's own tests; a failure here means a corrupted
		// build, not a runtime condition this cheap enumeration helper is
		// expected to recover from.
		panic(fmt.Errorf("mcpserver: load agent role registry: %w", err))
	}
	registerPublicTools(server, a, idx, agentReg)
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
