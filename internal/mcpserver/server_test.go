package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
)

// newTestApp builds a real *app.App rooted at a throwaway workspace with
// one git repository, mirroring cmd/punakawan/main_test.go's
// newSmokeWorkspace.
func newTestApp(t *testing.T) *app.App {
	t.Helper()

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

	server, err := newServer(a)
	if err != nil {
		t.Fatalf("newServer: %v", err)
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

	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal structured content into %T: %v", out, err)
	}
}

// TestServerInstructionsCoverApprovalAndPipeline guards the one piece of
// automatic agent guidance punakawan can give regardless of which project
// it's attached to (InitializeResult.Instructions) - it must mention both
// the expected tool call sequence and how to grant a write approval, since
// skipping the former and not knowing the latter is exactly what caused
// real usage to stall.
func TestServerInstructionsCoverApprovalAndPipeline(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	instructions := cs.InitializeResult().Instructions
	if instructions == "" {
		t.Fatal("Instructions is empty")
	}
	for _, want := range []string{"create_workflow_run", "punakawan approvals"} {
		if !strings.Contains(instructions, want) {
			t.Errorf("Instructions does not mention %q:\n%s", want, instructions)
		}
	}
}

func TestSemarPromptServesEmbeddedTemplate(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	res, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "semar"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("Messages = %+v, want 1", res.Messages)
	}
	text, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content = %T, want *mcp.TextContent", res.Messages[0].Content)
	}
	if !strings.Contains(text.Text, "You are **Semar**") {
		t.Fatalf("prompt text does not look like the Semar template: %q", text.Text[:min(80, len(text.Text))])
	}
}

func TestWorkflowRunLifecycle(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var created map[string]any
	callTool(t, cs, "create_workflow_run", map[string]any{
		"run_id":        "run-1",
		"workflow_name": "feature-delivery",
	}, &created)
	if created["state"] != "created" {
		t.Fatalf("created state = %v, want created", created["state"])
	}

	var got map[string]any
	callTool(t, cs, "get_workflow_state", map[string]any{"run_id": "run-1"}, &got)
	if got["state"] != "created" {
		t.Fatalf("get state = %v, want created", got["state"])
	}

	var advanced map[string]any
	callTool(t, cs, "advance_workflow", map[string]any{
		"run_id":     "run-1",
		"next_state": "context-building",
		"note":       "collecting sources",
	}, &advanced)
	if advanced["state"] != "context-building" {
		t.Fatalf("advanced state = %v, want context-building", advanced["state"])
	}
	checkpoints, ok := advanced["checkpoints"].([]any)
	if !ok || len(checkpoints) != 2 {
		t.Fatalf("checkpoints = %+v, want 2 entries", advanced["checkpoints"])
	}
}

func TestAdvanceWorkflowRejectsUnknownState(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var created map[string]any
	callTool(t, cs, "create_workflow_run", map[string]any{
		"run_id":        "run-1",
		"workflow_name": "feature-delivery",
	}, &created)

	ctx := context.Background()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "advance_workflow",
		Arguments: map[string]any{
			"run_id":     "run-1",
			"next_state": "bogus",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result for an unknown next_state")
	}
}

func requireDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
}

func TestSubmitGarengReviewPersists(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "submit_gareng_review", map[string]any{
		"id":    "run-1",
		"title": "Gareng review",
		"review": map[string]any{
			"verdict":           "clarification_required",
			"blocking_findings": []string{"no rollback plan"},
		},
	}, &out)

	if out["type"] != "gareng-review" {
		t.Fatalf("type = %v, want gareng-review", out["type"])
	}

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec, err := store.Get(out["id"].(string))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.GarengReview == nil || *rec.GarengReview.Verdict != "clarification_required" {
		t.Fatalf("GarengReview = %+v, want verdict clarification_required", rec.GarengReview)
	}
}

func TestSubmitSemarSynthesisRejectsBothPayloads(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	ctx := context.Background()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "submit_semar_synthesis",
		Arguments: map[string]any{
			"id":         "run-1",
			"title":      "synthesis",
			"synthesis":  map[string]any{"goal": "ship it"},
			"final_plan": map[string]any{"requirements": []string{"r1"}, "acceptance_criteria": []string{"a1"}},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error result when both synthesis and final_plan are set")
	}
}

func TestBuildContextDossierPersists(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "build_context_dossier", map[string]any{
		"run_id":    "run-1",
		"user_goal": "ship refunds",
	}, &out)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec, err := store.Get(out["id"].(string))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.ContextDossier == nil || *rec.ContextDossier.UserGoal != "ship refunds" {
		t.Fatalf("ContextDossier = %+v, want user_goal ship refunds", rec.ContextDossier)
	}
}
