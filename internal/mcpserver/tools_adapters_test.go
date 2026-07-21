package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
)

const prototypeAdapterPath = "../../packages/adapter-sdk/dist/prototypeAdapter.js"

// newTestAppWithPrototypeAdapter builds a real *app.App (mirroring
// newTestApp) whose workspace.yaml also registers the M0 prototype adapter
// under id "prototype", so call_adapter_operation can be exercised against
// a real spawned adapter process rather than a fake.
func newTestAppWithPrototypeAdapter(t *testing.T) *app.App {
	t.Helper()

	absPrototypePath, err := filepath.Abs(prototypeAdapterPath)
	if err != nil {
		t.Fatalf("resolve prototype adapter path: %v", err)
	}
	if _, err := os.Stat(absPrototypePath); err != nil {
		t.Skipf("prototype adapter not built (%s): %v; run `pnpm --filter @punakawan/adapter-sdk build` first", absPrototypePath, err)
	}

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
	workspaceYAML := fmt.Sprintf(
		"version: punakawan.workspace/v1\nid: smoke\nname: Smoke\nrepositories:\n  - id: repo-a\n    path: ./repo-a\nadapters:\n  prototype:\n    command: node\n    args:\n      - %q\n",
		absPrototypePath,
	)
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

// TestCallAdapterOperationEndToEnd exercises call_adapter_operation over the
// real MCP wire protocol: the tool must start the prototype adapter process
// on first use, discover its manifest via capabilities, complete the
// initialize handshake, and invoke its (approval-free) "sleep" operation,
// returning the adapter's own JSON result unchanged.
func TestCallAdapterOperationEndToEnd(t *testing.T) {
	a := newTestAppWithPrototypeAdapter(t)
	cs := connect(t, a)

	var out CallAdapterOperationOutput
	callTool(t, cs, "call_adapter_operation", map[string]any{
		"run_id":       "run-1",
		"adapter_id":   "prototype",
		"op":           "sleep",
		"params":       map[string]any{"ms": 0},
		"requested_by": "petruk",
	}, &out)

	if ok, _ := out.Result["ok"].(bool); !ok {
		t.Fatalf("Result = %+v, want ok=true", out.Result)
	}
}

// TestCallAdapterOperationUnknownAdapterID confirms an unregistered adapter
// id surfaces as a tool error rather than a hang or a silent no-op.
func TestCallAdapterOperationUnknownAdapterID(t *testing.T) {
	a := newTestAppWithPrototypeAdapter(t)
	cs := connect(t, a)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "call_adapter_operation",
		Arguments: map[string]any{
			"run_id":       "run-1",
			"adapter_id":   "does-not-exist",
			"op":           "sleep",
			"requested_by": "petruk",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for an unknown adapter id")
	}
}
