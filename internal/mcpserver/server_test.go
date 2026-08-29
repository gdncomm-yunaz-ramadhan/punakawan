package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
)

// newTestApp builds a real *app.App rooted at a throwaway workspace with
// one git repository, mirroring cmd/punakawan/main_test.go's
// newSmokeWorkspace.
//
// PUNAKAWAN_DATA_DIR is set to an isolated temp directory so any call
// through a.OpenStorage never touches this machine's real, shared
// database - without this override, every test using it would open the
// same on-disk database this developer's real Punakawan install uses.
func newTestApp(t *testing.T) *app.App {
	t.Helper()
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())

	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo-a")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir repo-a: %v", err)
	}
	runGit(t, repoDir, "init", "-q", "-b", "main")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}
	runGit(t, repoDir, "add", "f.txt")
	runGit(t, repoDir, "commit", "-q", "-m", "init")

	punakawanDir := filepath.Join(dir, ".punakawan")
	if err := os.MkdirAll(punakawanDir, 0o755); err != nil {
		t.Fatalf("mkdir .punakawan: %v", err)
	}
	workspaceYAML := "version: punakawan.workspace/v1\nid: smoke\nname: Smoke\nrepositories:\n  - id: repo-a\n    path: ./repo-a\n"
	if err := os.WriteFile(filepath.Join(punakawanDir, "workspace.yaml"), []byte(workspaceYAML), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}

	a, err := app.Load(dir)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return a
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// connect builds the server for a and connects a client to it over an
// in-memory transport, returning the client session.
func connect(t *testing.T, a *app.App) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server, _, err := assembleServer(a)
	if err != nil {
		t.Fatalf("assembleServer: %v", err)
	}

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })

	return clientSession
}

// listToolNames returns cs's currently visible tool names.
func listToolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	return names
}

func TestToolListIsFocusedPublicSurface(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	names := listToolNames(t, cs)
	want := map[string]bool{
		"upsert_project": true, "list_projects": true,
		"save_workflow": true, "get_workflow": true, "list_workflows": true, "invoke_workflow": true,
		"plan_save": true, "plan_get": true,
		"start_delivery": true, "start_delivery_session": true,
		"checkpoint_delivery_session": true, "report_delivery_usage": true, "report_delivery_progress": true,
		"assess_jira_delivery": true, "hydrate_jira_delivery": true, "hydrate_github_pull_request": true, "propose_github_pr_review": true, "get_github_pr_review": true, "submit_github_pr_review": true, "queue_jira_write": true,
		"execute_jira_writes": true, "map_delivery_work_item": true,
		"get_delivery": true, "answer_delivery_question": true, "log_delivery_work": true,
		"cancel_delivery": true, "complete_delivery": true, "retry_worklog_sync": true, "cancel_jira_write_intent": true,
	}
	if len(names) != len(want) {
		t.Fatalf("tools/list = %d tools %v, want exactly %d tools %v", len(names), names, len(want), want)
	}
	for name := range want {
		if !names[name] {
			t.Errorf("public tool %q missing from tools/list", name)
		}
	}
}

// TestPublicToolsContainNoExecutionApprovalTools guards the removal of the
// execution-approval contract: no tool that would let a caller gate or grant
// authorization for adapter writes is exposed on the public MCP surface.
func TestPublicToolsContainNoExecutionApprovalTools(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	names := listToolNames(t, cs)
	for _, forbidden := range []string{"approve_jira_delivery", "approve_github_pr_review"} {
		if names[forbidden] {
			t.Fatalf("obsolete approval tool %q is registered", forbidden)
		}
	}
}

// callTool invokes a tool and decodes its structured output into out.
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) returned an error result: %+v", name, res.Content)
	}
	if len(res.Content) != 1 {
		t.Fatalf("CallTool(%s) content blocks = %d, want JSON result", name, len(res.Content))
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s) content = %+v, want text JSON", name, res.Content)
	}
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if text.Text != string(data) {
		t.Fatalf("CallTool(%s) content = %q, want structured JSON", name, text.Text)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal structured content into %T: %v", out, err)
	}
}

// TestToolListSchemasHaveNoBareBooleanSubschemas guards against a regression
// that made every tool invisible to Claude Code's ToolSearch even though the
// server's wire response was byte-for-byte valid MCP: a Go `any`/`interface{}`
// field with no jsonschema tag (e.g. SetProjectMetadataOutput.Value) makes
// jsonschema-go emit a bare JSON boolean (true) for that property instead of
// an object schema - spec-legal, but rejected by Claude Code's client
// ("Invalid input (at tools.N.outputSchema.properties.value)"), which fails
// the whole tools/list response, not just the one offending tool. This walks
// every registered tool's full input/output schema tree (not just top-level
// properties - the same defect can recur nested under items/properties/etc)
// looking for a raw JSON boolean anywhere a schema is expected.
func TestToolListSchemasHaveNoBareBooleanSubschemas(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) == 0 {
		t.Fatal("ListTools returned no tools")
	}

	for _, tool := range res.Tools {
		for _, schema := range []any{tool.InputSchema, tool.OutputSchema} {
			if schema == nil {
				continue
			}
			raw, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("%s: marshal schema: %v", tool.Name, err)
			}
			var decoded any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("%s: unmarshal schema: %v", tool.Name, err)
			}
			if hits := findBoolSchemaNodes(decoded, ""); len(hits) > 0 {
				t.Errorf("%s: bare-boolean schema node(s) found: %v", tool.Name, hits)
			}
		}
	}
}

// singleSchemaKeys are JSON Schema keywords whose value is exactly one
// (sub)schema - Claude Code's client requires an object there, not a bare
// boolean, even though a bare boolean is spec-legal shorthand for it.
var singleSchemaKeys = map[string]bool{
	"items": true, "additionalItems": true, "contains": true,
	"unevaluatedItems": true, "additionalProperties": true,
	"propertyNames": true, "unevaluatedProperties": true,
	"not": true, "if": true, "then": true, "else": true,
	"contentSchema": true,
}

// mapOfSchemaKeys are keywords whose value is a map from name to subschema.
var mapOfSchemaKeys = map[string]bool{
	"properties": true, "patternProperties": true,
	"dependentSchemas": true, "$defs": true, "definitions": true,
}

// listOfSchemaKeys are keywords whose value is a list of subschemas.
var listOfSchemaKeys = map[string]bool{
	"allOf": true, "anyOf": true, "oneOf": true, "prefixItems": true,
}

// findBoolSchemaNodes recursively finds every raw JSON boolean found at a
// schema-valued position in a decoded schema tree (properties, items,
// additionalProperties, ...), returning each occurrence's path for a
// legible failure message. Keys like "uniqueItems" or "deprecated" hold
// real, harmless JSON booleans and are deliberately not schema positions,
// so they are never flagged.
func findBoolSchemaNodes(node any, path string) []string {
	m, ok := node.(map[string]any)
	if !ok {
		return nil
	}
	var hits []string
	for k, v := range m {
		switch {
		case singleSchemaKeys[k]:
			hits = append(hits, checkSchema(v, path+"."+k)...)
		case mapOfSchemaKeys[k]:
			if sub, ok := v.(map[string]any); ok {
				for name, child := range sub {
					hits = append(hits, checkSchema(child, path+"."+k+"."+name)...)
				}
			}
		case listOfSchemaKeys[k]:
			if list, ok := v.([]any); ok {
				for i, child := range list {
					hits = append(hits, checkSchema(child, fmt.Sprintf("%s.%s[%d]", path, k, i))...)
				}
			}
		}
	}
	return hits
}

// checkSchema reports v's own path if v is a bare boolean, and always
// recurses to find further nested schema-valued positions when v is an
// object schema.
func checkSchema(v any, path string) []string {
	if b, ok := v.(bool); ok {
		return []string{path + "=" + strconv.FormatBool(b)}
	}
	return findBoolSchemaNodes(v, path)
}
