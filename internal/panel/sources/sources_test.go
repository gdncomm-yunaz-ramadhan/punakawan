package sources

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func requireDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
}

func requireBd(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// newTestApp builds a real *app.App rooted at a throwaway workspace with
// one git repository and (if bd is installed) an initialized bd project,
// mirroring internal/mcpserver/server_test.go's newTestApp.
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

	if _, err := exec.LookPath("bd"); err == nil {
		res, err := a.Supervisor.Run(context.Background(), tools.Spec{
			Name: "bd",
			Args: []string{"init", "--non-interactive", "--prefix", "test", "--skip-agents", "--skip-hooks", "-q"},
			Dir:  dir,
		})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("bd init: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
		}
	}

	return a
}

func TestWorkspaceSourceGetDescribesCurrentWorkspace(t *testing.T) {
	requireBd(t)
	a := newTestApp(t)
	ws := &WorkspaceSource{App: a}

	detail, err := ws.Get(context.Background(), a.Workspace.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.ID != a.Workspace.ID {
		t.Fatalf("ID = %q, want %q", detail.ID, a.Workspace.ID)
	}
	if detail.RepositoryCount != 1 {
		t.Fatalf("RepositoryCount = %d, want 1", detail.RepositoryCount)
	}
	if len(detail.Health) == 0 {
		t.Fatal("expected at least one source health entry")
	}
}

func TestWorkspaceSourceGetRejectsUnknownWorkspace(t *testing.T) {
	a := newTestApp(t)
	ws := &WorkspaceSource{App: a}

	if _, err := ws.Get(context.Background(), "some-other-workspace"); err == nil {
		t.Fatal("expected an error for a workspace this app was not loaded for")
	}
}

func TestWorkspaceSourceListReturnsOneEntry(t *testing.T) {
	a := newTestApp(t)
	ws := &WorkspaceSource{App: a}

	summaries, err := ws.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("List returned %d entries, want 1", len(summaries))
	}
}

func newTestRun(a *app.App, id string) protocol.WorkflowRun {
	return workflow.New(id, a.Workspace.ID, protocol.WorkflowRunWorkflowNameFeatureDelivery, time.Now().UTC())
}

func TestSessionSourceListAndGet(t *testing.T) {
	a := newTestApp(t)
	run := newTestRun(a, "run-test-1")
	if err := a.Workflow.Append(run); err != nil {
		t.Fatalf("Workflow.Append: %v", err)
	}

	ss := &SessionSource{App: a}

	summaries, err := ss.List(context.Background(), a.Workspace.ID, contract.SessionFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Id != run.Id {
		t.Fatalf("List = %+v, want one summary for %s", summaries, run.Id)
	}

	detail, err := ss.Get(context.Background(), a.Workspace.ID, run.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.Id != run.Id {
		t.Fatalf("Get.Id = %q, want %q", detail.Id, run.Id)
	}
}

func TestSessionSourceListFiltersByStatus(t *testing.T) {
	a := newTestApp(t)
	if err := a.Workflow.Append(newTestRun(a, "run-test-1")); err != nil {
		t.Fatalf("Workflow.Append: %v", err)
	}

	ss := &SessionSource{App: a}
	summaries, err := ss.List(context.Background(), a.Workspace.ID, contract.SessionFilter{Status: "completed"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("List with status=completed = %+v, want none", summaries)
	}
}

func TestTaskSourceListGetDependencies(t *testing.T) {
	requireBd(t)
	a := newTestApp(t)
	ts := &TaskSource{App: a}
	ctx := context.Background()

	res, err := a.Supervisor.Run(ctx, tools.Spec{
		Name: "bd",
		Args: []string{"create", "--json", "--title=first task", "--type=task"},
		Dir:  a.Workspace.Root,
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("bd create: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
	}

	issues, err := ts.List(ctx, a.Workspace.ID, contract.TaskFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("List = %+v, want 1 issue", issues)
	}

	detail, err := ts.Get(ctx, a.Workspace.ID, issues[0].ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if detail.ID != issues[0].ID {
		t.Fatalf("Get.ID = %q, want %q", detail.ID, issues[0].ID)
	}

	graph, err := ts.Dependencies(ctx, a.Workspace.ID)
	if err != nil {
		t.Fatalf("Dependencies: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("Dependencies.Nodes = %+v, want 1", graph.Nodes)
	}
}

func TestKnowledgeSourceSearchGetRelations(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	ks := &KnowledgeSource{App: a}
	ctx := context.Background()

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id:     "pkw:requirement/repo-a/refund-sla",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Refund SLA policy",
		Source: protocol.KnowledgeRecordSource{Provider: "manual", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State:      protocol.KnowledgeRecordValidityStateVerified,
			VerifiedBy: []string{"test"},
		},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := ks.Get(ctx, a.Workspace.ID, rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Id != rec.Id {
		t.Fatalf("Get.Id = %q, want %q", got.Id, rec.Id)
	}

	if _, err := ks.Relations(ctx, a.Workspace.ID, rec.Id); err != nil {
		t.Fatalf("Relations: %v", err)
	}

	ix, err := a.OpenSearchIndex()
	if err != nil {
		t.Fatalf("OpenSearchIndex: %v", err)
	}
	if err := search.Rebuild(store, ix); err != nil {
		t.Fatalf("search.Rebuild: %v", err)
	}

	results, err := ks.Search(ctx, a.Workspace.ID, search.Request{Query: "refund SLA"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result")
	}
}

func TestApprovalSourceListFiltersByStatus(t *testing.T) {
	a := newTestApp(t)
	as := &ApprovalSource{App: a}

	rec := protocol.ApprovalRecord{
		Id:          "appr-1",
		RunId:       "run-1",
		Operation:   protocol.ApprovalRecordOperationGitPush,
		RequestedBy: protocol.ApprovalRecordRequestedByPetruk,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := a.Approvals.Append(rec); err != nil {
		t.Fatalf("Approvals.Append: %v", err)
	}

	all, err := as.List(context.Background(), a.Workspace.ID, contract.ApprovalFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("List = %+v, want 1", all)
	}

	pending, err := as.List(context.Background(), a.Workspace.ID, contract.ApprovalFilter{Status: "pending"})
	if err != nil {
		t.Fatalf("List(pending): %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("List(pending) = %+v, want 1", pending)
	}

	approved, err := as.List(context.Background(), a.Workspace.ID, contract.ApprovalFilter{Status: "approved"})
	if err != nil {
		t.Fatalf("List(approved): %v", err)
	}
	if len(approved) != 0 {
		t.Fatalf("List(approved) = %+v, want 0", approved)
	}
}
