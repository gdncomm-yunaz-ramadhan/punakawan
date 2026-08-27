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
// actually tripped up real usage: the expected tool call sequence and durable
// retry behavior.
const serverInstructionsBody = `Punakawan is a focused multi-project delivery orchestrator. Work through Projects, Workflows, Plans, and Deliveries. Start or resume work with start_delivery, invoke_workflow, or get_delivery. When work is complete on an exact Jira task, record its measured task-bound interval with log_delivery_work before reporting the lane complete. Retry an unsynced interval with retry_worklog_sync rather than recording it again. Report provider-observed model usage with report_delivery_usage, including current unit price and price source whenever connected agent can obtain them; never ask humans to maintain price tables. To assess a Jira issue: resolve it, hydrate its parent and every subtask, reason over visible source, then record clarity and rationale. Propose parent Fibonacci story points from total subtask complexity and lower agent-assisted original estimates per subtask from expected execution time. Queue Jira writes as durable intents, then execute one by intent_id or all pending intents by execution_id. Cancel stale pending intents with cancel_jira_write_intent. Queueing story points discovers and caches field metadata by cloud, project, and issue type; use refresh_story_points_field after a Jira field change. Runtime mechanics and provider operations stay delegated to connected adapters. Punakawan does not reason itself; connected agent remains reasoning engine.`

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
	idx := newToolIndex()
	registerPublicTools(server, a, idx)

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
	registerPublicTools(server, a, idx)
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
