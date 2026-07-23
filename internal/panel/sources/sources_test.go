package sources

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/registry"
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

func strPtr(s string) *string { return &s }

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

func openTestRegistry(t *testing.T) *registry.Store {
	t.Helper()
	reg, err := registry.OpenAt(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatalf("registry.OpenAt: %v", err)
	}
	return reg
}

func TestWorkspaceSourceListWithRegistryDescribesAllEntries(t *testing.T) {
	a := newTestApp(t)
	reg := openTestRegistry(t)
	if _, err := reg.Register(a.Workspace.ID, a.Workspace.Root, "", time.Now().UTC()); err != nil {
		t.Fatalf("Register (current): %v", err)
	}

	otherDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(otherDir, ".punakawan"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, ".punakawan", "workspace.yaml"),
		[]byte("version: punakawan.workspace/v1\nid: other\nname: Other\nrepositories:\n  - id: r\n    path: .\n"), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}
	runGit(t, otherDir, "init", "-q", "-b", "main")
	runGit(t, otherDir, "config", "user.email", "test@example.com")
	runGit(t, otherDir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(otherDir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write f.txt: %v", err)
	}
	runGit(t, otherDir, "add", "f.txt")
	runGit(t, otherDir, "commit", "-q", "-m", "init")
	if _, err := reg.Register("other", otherDir, "Other", time.Now().UTC()); err != nil {
		t.Fatalf("Register (other): %v", err)
	}

	ws := &WorkspaceSource{App: a, Registry: reg}
	summaries, err := ws.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("List = %+v, want 2 entries", summaries)
	}

	var ids []string
	for _, s := range summaries {
		ids = append(ids, s.ID)
	}
	if !contains(ids, a.Workspace.ID) || !contains(ids, "other") {
		t.Fatalf("ids = %v, want both %q and \"other\"", ids, a.Workspace.ID)
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func TestWorkspaceSourceListDegradesBrokenPathToUnavailable(t *testing.T) {
	a := newTestApp(t)
	reg := openTestRegistry(t)
	if _, err := reg.Register(a.Workspace.ID, a.Workspace.Root, "", time.Now().UTC()); err != nil {
		t.Fatalf("Register (current): %v", err)
	}

	// Register a second workspace, then delete its directory so its path
	// becomes broken - List must still return the current workspace's
	// summary, marking the broken one unavailable instead of erroring.
	brokenDir := t.TempDir()
	if _, err := reg.Register("broken", brokenDir, "Broken", time.Now().UTC()); err != nil {
		t.Fatalf("Register (broken): %v", err)
	}
	if err := os.RemoveAll(brokenDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	ws := &WorkspaceSource{App: a, Registry: reg}
	summaries, err := ws.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("List = %+v, want 2 entries (one degraded, not dropped)", summaries)
	}

	var broken *contract.WorkspaceSummary
	for i := range summaries {
		if summaries[i].ID == "broken" {
			broken = &summaries[i]
		}
	}
	if broken == nil {
		t.Fatalf("List = %+v, want a \"broken\" entry", summaries)
	}
	if broken.Availability != protocol.PanelSourceHealthAvailabilityUnavailable {
		t.Fatalf("broken.Availability = %q, want unavailable", broken.Availability)
	}
}

func TestWorkspaceSourceGetUnknownIDErrorsEvenWithRegistry(t *testing.T) {
	a := newTestApp(t)
	reg := openTestRegistry(t)
	ws := &WorkspaceSource{App: a, Registry: reg}

	if _, err := ws.Get(context.Background(), "no-such-workspace"); err == nil {
		t.Fatal("expected an error for a workspace that is not in the registry at all")
	}
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
	if len(graph.Cycles) != 0 {
		t.Fatalf("Dependencies.Cycles = %+v, want none", graph.Cycles)
	}

	matches, err := ts.List(ctx, a.Workspace.ID, contract.TaskFilter{Query: "first"})
	if err != nil {
		t.Fatalf("List with query: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("List with query=first = %+v, want 1", matches)
	}
	none, err := ts.List(ctx, a.Workspace.ID, contract.TaskFilter{Query: "no-such-task-title"})
	if err != nil {
		t.Fatalf("List with non-matching query: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("List with non-matching query = %+v, want none", none)
	}
}

func TestTaskSourceDependenciesDetectsCycle(t *testing.T) {
	requireBd(t)
	a := newTestApp(t)
	ts := &TaskSource{App: a}
	ctx := context.Background()

	create := func(title string) string {
		res, err := a.Supervisor.Run(ctx, tools.Spec{
			Name: "bd",
			Args: []string{"create", "--json", "--title=" + title, "--type=task"},
			Dir:  a.Workspace.Root,
		})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("bd create %s: err=%v exit=%d stderr=%s", title, err, res.ExitCode, res.Stderr)
		}
		var out struct {
			Id string `json:"id"`
		}
		if err := json.Unmarshal(res.Stdout, &out); err != nil {
			t.Fatalf("decode bd create output: %v", err)
		}
		return out.Id
	}
	addDep := func(from, to string) {
		res, err := a.Supervisor.Run(ctx, tools.Spec{
			Name: "bd",
			Args: []string{"dep", "add", from, to, "--json", "--type", "related"},
			Dir:  a.Workspace.Root,
		})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("bd dep add %s %s: err=%v exit=%d stderr=%s", from, to, err, res.ExitCode, res.Stderr)
		}
	}

	first := create("cycle task one")
	second := create("cycle task two")
	// bd itself rejects a dependency that would create a cycle through its
	// own "blocks" type, so this uses "related" edges (which bd allows to
	// cycle) purely to exercise detectCycles against a real bd-shaped
	// edge list.
	addDep(first, second)
	addDep(second, first)

	graph, err := ts.Dependencies(ctx, a.Workspace.ID)
	if err != nil {
		t.Fatalf("Dependencies: %v", err)
	}
	if len(graph.Cycles) == 0 {
		t.Fatalf("Dependencies.Cycles = %+v, want at least one cycle for %s <-> %s", graph.Cycles, first, second)
	}
}

func TestTaskSourceListComputesRealBlockedStatusFromReadiness(t *testing.T) {
	requireBd(t)
	a := newTestApp(t)
	ts := &TaskSource{App: a}
	ctx := context.Background()

	create := func(title string) string {
		res, err := a.Supervisor.Run(ctx, tools.Spec{
			Name: "bd",
			Args: []string{"create", "--json", "--title=" + title, "--type=task"},
			Dir:  a.Workspace.Root,
		})
		if err != nil || res.ExitCode != 0 {
			t.Fatalf("bd create %s: err=%v exit=%d stderr=%s", title, err, res.ExitCode, res.Stderr)
		}
		var out struct {
			Id string `json:"id"`
		}
		if err := json.Unmarshal(res.Stdout, &out); err != nil {
			t.Fatalf("decode bd create output: %v", err)
		}
		return out.Id
	}

	epicRes, err := a.Supervisor.Run(ctx, tools.Spec{
		Name: "bd",
		Args: []string{"create", "--json", "--title=parent epic", "--type=epic"},
		Dir:  a.Workspace.Root,
	})
	if err != nil || epicRes.ExitCode != 0 {
		t.Fatalf("bd create epic: err=%v exit=%d stderr=%s", err, epicRes.ExitCode, epicRes.Stderr)
	}
	var epic struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(epicRes.Stdout, &epic); err != nil {
		t.Fatalf("decode bd create epic output: %v", err)
	}

	prereq := create("prerequisite task")
	res, err := a.Supervisor.Run(ctx, tools.Spec{
		Name: "bd",
		// Created as a child of an open epic, so its Dependencies also
		// carries a parent-child edge to an issue that is not closed -
		// this must NOT count as a blocking reason (see blockingReasons'
		// doc comment: only "blocks" edges do, verified against bd
		// ready --explain).
		Args: []string{"create", "--json", "--title=dependent task", "--type=task", "--parent", epic.Id},
		Dir:  a.Workspace.Root,
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("bd create dependent: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
	}
	var dependentOut struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(res.Stdout, &dependentOut); err != nil {
		t.Fatalf("decode bd create dependent output: %v", err)
	}
	dependent := dependentOut.Id

	depRes, err := a.Supervisor.Run(ctx, tools.Spec{
		Name: "bd",
		Args: []string{"dep", "add", dependent, prereq, "--json", "--type", "blocks"},
		Dir:  a.Workspace.Root,
	})
	if err != nil || depRes.ExitCode != 0 {
		t.Fatalf("bd dep add: err=%v exit=%d stderr=%s", err, depRes.ExitCode, depRes.Stderr)
	}

	issues, err := ts.List(ctx, a.Workspace.ID, contract.TaskFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := map[string]contract.TaskSummary{}
	for _, issue := range issues {
		byID[issue.ID] = issue
	}

	dependentSummary, ok := byID[dependent]
	if !ok {
		t.Fatalf("List = %+v, missing dependent task %s", issues, dependent)
	}
	// bd itself never flips the dependent's stored status away from
	// "open" just because prereq is still open (verified empirically) -
	// this is exactly the case BoardStatus must catch via real readiness.
	if dependentSummary.Status != "open" {
		t.Fatalf("dependent Status = %q, want open (bd does not auto-set status=blocked)", dependentSummary.Status)
	}
	if dependentSummary.BoardStatus != "blocked" {
		t.Fatalf("dependent BoardStatus = %q, want blocked", dependentSummary.BoardStatus)
	}
	if len(dependentSummary.BlockingReasons) != 1 {
		t.Fatalf("dependent BlockingReasons = %+v, want 1 entry naming %s", dependentSummary.BlockingReasons, prereq)
	}

	prereqSummary, ok := byID[prereq]
	if !ok {
		t.Fatalf("List = %+v, missing prerequisite task %s", issues, prereq)
	}
	if prereqSummary.BoardStatus != "ready" {
		t.Fatalf("prerequisite BoardStatus = %q, want ready", prereqSummary.BoardStatus)
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

func TestKnowledgeSourceListFiltersAndHistory(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	ks := &KnowledgeSource{App: a}
	ctx := context.Background()

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}

	verified := protocol.KnowledgeRecord{
		Id:         "pkw:requirement/repo-a/refund-sla",
		Type:       protocol.KnowledgeRecordTypeRequirement,
		Status:     "active",
		Title:      "Refund SLA policy",
		Source:     protocol.KnowledgeRecordSource{Provider: "manual", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodModelAssisted},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateVerified, VerifiedBy: []string{"test"}},
		Scope:      &protocol.KnowledgeRecordScope{Repository: strPtr("repo-a")},
	}
	if err := store.Put(verified); err != nil {
		t.Fatalf("Put verified: %v", err)
	}

	stale := protocol.KnowledgeRecord{
		Id:         "pkw:requirement/repo-b/checkout-flow",
		Type:       protocol.KnowledgeRecordTypeRequirement,
		Status:     "active",
		Title:      "Checkout flow",
		Source:     protocol.KnowledgeRecordSource{Provider: "jira", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodModelAssisted},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateStale},
		Scope:      &protocol.KnowledgeRecordScope{Repository: strPtr("repo-b")},
	}
	if err := store.Put(stale); err != nil {
		t.Fatalf("Put stale: %v", err)
	}

	all, err := ks.List(ctx, a.Workspace.ID, contract.KnowledgeFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List = %+v, want 2", all)
	}

	byRepo, err := ks.List(ctx, a.Workspace.ID, contract.KnowledgeFilter{Repository: "repo-a"})
	if err != nil {
		t.Fatalf("List by repository: %v", err)
	}
	if len(byRepo) != 1 || byRepo[0].Id != verified.Id {
		t.Fatalf("List by repository=repo-a = %+v, want only %s", byRepo, verified.Id)
	}

	staleOnly, err := ks.List(ctx, a.Workspace.ID, contract.KnowledgeFilter{Stale: true})
	if err != nil {
		t.Fatalf("List stale=true: %v", err)
	}
	if len(staleOnly) != 1 || staleOnly[0].Id != stale.Id {
		t.Fatalf("List stale=true = %+v, want only %s", staleOnly, stale.Id)
	}

	bySource, err := ks.List(ctx, a.Workspace.ID, contract.KnowledgeFilter{Source: "jira"})
	if err != nil {
		t.Fatalf("List by source: %v", err)
	}
	if len(bySource) != 1 || bySource[0].Id != stale.Id {
		t.Fatalf("List source=jira = %+v, want only %s", bySource, stale.Id)
	}

	if err := store.Supersede(verified.Id, stale.Id); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	history, err := ks.History(ctx, a.Workspace.ID, verified.Id)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	// Supersede's own implementation Puts the record back through the
	// same path any other write takes (setting SupersededBy first), so it
	// emits both a "put" and a "supersede" event - the coarseness
	// documented on KnowledgeReader.History: a "put" alone cannot tell a
	// create from a later update.
	if len(history) != 3 {
		t.Fatalf("History = %+v, want 3 events (create put, supersede's put, supersede)", history)
	}
	last := history[len(history)-1]
	if last.Type != knowledge.EventTypeSupersede || last.SupersededBy != stale.Id {
		t.Fatalf("last History entry = %+v, want a supersede event naming %s", last, stale.Id)
	}

	noHistory, err := ks.History(ctx, a.Workspace.ID, "pkw:requirement/repo-a/never-existed")
	if err != nil {
		t.Fatalf("History for unknown id: %v", err)
	}
	if len(noHistory) != 0 {
		t.Fatalf("History for unknown id = %+v, want none", noHistory)
	}
}

func TestGlobalSearchSourceFusesAcrossWorkspaces(t *testing.T) {
	requireDolt(t)
	requireBd(t)
	a := newTestApp(t)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id:         "pkw:requirement/repo-a/refund-sla",
		Type:       protocol.KnowledgeRecordTypeRequirement,
		Status:     "active",
		Title:      "Refund SLA policy",
		Source:     protocol.KnowledgeRecordSource{Provider: "manual", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodModelAssisted},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateVerified, VerifiedBy: []string{"test"}},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	ix, err := a.OpenSearchIndex()
	if err != nil {
		t.Fatalf("OpenSearchIndex: %v", err)
	}
	if err := search.Rebuild(store, ix); err != nil {
		t.Fatalf("search.Rebuild: %v", err)
	}

	gs := &GlobalSearchSource{App: a, Registry: nil}
	results, err := gs.Search(context.Background(), search.Request{Query: "refund SLA"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one fused result")
	}
	if results[0].WorkspaceID != a.Workspace.ID {
		t.Fatalf("results[0].WorkspaceID = %q, want %q", results[0].WorkspaceID, a.Workspace.ID)
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

func TestWorkspaceSourceGetIncludesGitHealth(t *testing.T) {
	requireBd(t)
	a := newTestApp(t)
	ws := &WorkspaceSource{App: a}

	detail, err := ws.Get(context.Background(), a.Workspace.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	found := false
	for _, h := range detail.Health {
		if h.Source == "git:repo-a" {
			found = true
			if h.Availability != protocol.PanelSourceHealthAvailabilityAvailable {
				t.Fatalf("git:repo-a availability = %s, want available", h.Availability)
			}
		}
	}
	if !found {
		t.Fatalf("Health = %+v, want a git:repo-a entry", detail.Health)
	}
}

// writeEvidenceFile writes content under
// <workspaceRoot>/.punakawan/evidence/<runID>/, appends a matching
// EvidenceRecord to that run's ledger, and registers a workflow run for
// runID so EvidenceSource.Get (which enumerates known runs) can find it.
// It returns the absolute path written.
func writeEvidenceFile(t *testing.T, a *app.App, runID, evidenceID, name, content string, evidenceType protocol.EvidenceRecordType) string {
	t.Helper()
	if err := a.Workflow.Append(newTestRun(a, runID)); err != nil {
		t.Fatalf("Workflow.Append: %v", err)
	}

	dir := filepath.Join(a.Workspace.Root, ".punakawan", "evidence", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir evidence dir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write evidence file: %v", err)
	}

	ledger, err := evidence.OpenLedger(a.Workspace.Root, runID)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if err := ledger.Append(protocol.EvidenceRecord{
		Id:        evidenceID,
		RunId:     runID,
		Type:      evidenceType,
		Path:      &path,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ledger.Append: %v", err)
	}
	return path
}

// awsKeyLooking is built by concatenation, not as one contiguous literal,
// so this file's raw text doesn't contain a string shaped like a real AWS
// access key id - GitHub's push protection secret scanner flags that
// shape on sight, real or not, and a contiguous literal here blocks every
// push.
const awsKeyLooking = "AKIA" + "ABCDEFGHIJKLMNOP"

func TestEvidenceSourcePreviewRedactsAndSupportsRanges(t *testing.T) {
	a := newTestApp(t)
	writeEvidenceFile(t, a, "run-ev-1", "ev-1", "build.log",
		"line one\nAWS_ACCESS_KEY_ID="+awsKeyLooking+"\nline three\n",
		protocol.EvidenceRecordTypeCommandOutput)

	es := &EvidenceSource{App: a}

	full, err := es.Preview(context.Background(), a.Workspace.ID, "ev-1", 0, 0)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if strings.Contains(string(full.Data), awsKeyLooking) {
		t.Fatalf("Preview text = %q, still contains the secret", full.Data)
	}
	if !strings.Contains(string(full.Data), "[REDACTED]") {
		t.Fatalf("Preview text = %q, want a [REDACTED] marker", full.Data)
	}
	if full.Truncated {
		t.Fatalf("Truncated = true for a small file read in full")
	}

	ranged, err := es.Preview(context.Background(), a.Workspace.ID, "ev-1", 0, 9)
	if err != nil {
		t.Fatalf("Preview(limit=9): %v", err)
	}
	if string(ranged.Data) != "line one\n" {
		t.Fatalf("Preview(limit=9).Data = %q, want %q", ranged.Data, "line one\n")
	}
	if !ranged.Truncated {
		t.Fatal("Preview(limit=9): want Truncated=true")
	}
}

func TestEvidenceSourcePreviewRejectsPathOutsideEvidenceRoot(t *testing.T) {
	a := newTestApp(t)
	if err := a.Workflow.Append(newTestRun(a, "run-ev-2")); err != nil {
		t.Fatalf("Workflow.Append: %v", err)
	}
	ledger, err := evidence.OpenLedger(a.Workspace.Root, "run-ev-2")
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	escaped := filepath.Join(a.Workspace.Root, "repo-a", "f.txt")
	if err := ledger.Append(protocol.EvidenceRecord{
		Id:        "ev-escape",
		RunId:     "run-ev-2",
		Type:      protocol.EvidenceRecordTypeCommandOutput,
		Path:      &escaped,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ledger.Append: %v", err)
	}

	es := &EvidenceSource{App: a}
	if _, err := es.Preview(context.Background(), a.Workspace.ID, "ev-escape", 0, 0); err == nil {
		t.Fatal("Preview: expected an error for a path outside the evidence directory, got nil")
	}
}

func TestEvidenceSourcePreviewComputesDiffSummary(t *testing.T) {
	a := newTestApp(t)
	diff := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1,3 +1,4 @@\n package foo\n+import \"fmt\"\n-old line\n unchanged\n"
	writeEvidenceFile(t, a, "run-ev-3", "ev-diff", "diff.patch", diff, protocol.EvidenceRecordTypeGitDiff)

	es := &EvidenceSource{App: a}
	preview, err := es.Preview(context.Background(), a.Workspace.ID, "ev-diff", 0, 0)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.DiffSummary == nil {
		t.Fatal("DiffSummary = nil, want a summary for a git-diff evidence type")
	}
	if preview.DiffSummary.FilesChanged != 1 || preview.DiffSummary.Insertions != 1 || preview.DiffSummary.Deletions != 1 {
		t.Fatalf("DiffSummary = %+v, want {FilesChanged:1 Insertions:1 Deletions:1}", preview.DiffSummary)
	}
}

func TestEvidenceSourcePreviewServesScreenshotAsBinary(t *testing.T) {
	a := newTestApp(t)
	writeEvidenceFile(t, a, "run-ev-4", "ev-shot", "screen.png", "not-really-a-png-but-bytes", protocol.EvidenceRecordTypeScreenshot)

	es := &EvidenceSource{App: a}
	preview, err := es.Preview(context.Background(), a.Workspace.ID, "ev-shot", 0, 0)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if preview.Kind != "binary" {
		t.Fatalf("Kind = %q, want binary", preview.Kind)
	}
	if preview.MimeType != "image/png" {
		t.Fatalf("MimeType = %q, want image/png", preview.MimeType)
	}
	if string(preview.Data) != "not-really-a-png-but-bytes" {
		t.Fatalf("Data = %q, want the file's raw bytes", preview.Data)
	}
}
