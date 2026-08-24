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
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/prreview"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/testrun"
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
	// Isolate the shared SQLite kernel to a per-test temp dir so OpenKnowledge/
	// OpenTaskStore never touch this machine's real, shared database.
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
	// A dedicated, isolated storage kernel per call, so tests that do not
	// otherwise set PUNAKAWAN_DATA_DIR never share (and leak entries through)
	// this machine's real registry.
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "registry.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return registry.New(db)
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

	// Order preservation: List is parallelized (punokawan-d9h) but reassembles
	// by index, so the summaries must follow the registry's own entry order.
	entries, err := reg.List()
	if err != nil {
		t.Fatalf("reg.List: %v", err)
	}
	if len(entries) != len(summaries) {
		t.Fatalf("entry/summary count mismatch: %d vs %d", len(entries), len(summaries))
	}
	for i := range entries {
		if summaries[i].ID != entries[i].Id {
			t.Fatalf("order not preserved at %d: summary=%q registry=%q", i, summaries[i].ID, entries[i].Id)
		}
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

// TestSessionSourceListSkipCountsOmitsEvidenceCounts proves SkipCounts
// actually skips the per-run ledger/journal scan, rather than happening to
// return zero counts anyway: a real evidence record is written for the run,
// so a non-SkipCounts call must see it, and a SkipCounts call must not
// (Overview's whole reason for this flag - it never renders these counts,
// so paying for the scan was pure waste).
func TestSessionSourceListSkipCountsOmitsEvidenceCounts(t *testing.T) {
	a := newTestApp(t)
	run := newTestRun(a, "run-test-1")
	if err := a.Workflow.Append(run); err != nil {
		t.Fatalf("Workflow.Append: %v", err)
	}

	ledger, err := evidence.OpenLedger(a.Workspace.Root, run.Id)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if err := ledger.Append(protocol.EvidenceRecord{Id: "ev-1", RunId: run.Id, Type: protocol.EvidenceRecordTypeCommandOutput, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("ledger.Append: %v", err)
	}

	ss := &SessionSource{App: a}

	withCounts, err := ss.List(context.Background(), a.Workspace.ID, contract.SessionFilter{})
	if err != nil {
		t.Fatalf("List (with counts): %v", err)
	}
	if len(withCounts) != 1 || withCounts[0].EvidenceCount == nil || *withCounts[0].EvidenceCount != 1 {
		t.Fatalf("List (with counts) = %+v, want EvidenceCount=1", withCounts)
	}

	skipped, err := ss.List(context.Background(), a.Workspace.ID, contract.SessionFilter{SkipCounts: true})
	if err != nil {
		t.Fatalf("List (SkipCounts): %v", err)
	}
	if len(skipped) != 1 || skipped[0].EvidenceCount == nil || *skipped[0].EvidenceCount != 0 {
		t.Fatalf("List (SkipCounts) = %+v, want EvidenceCount=0 (counts skipped, not computed)", skipped)
	}
}

// TestSessionSourceCountsReflectCanonicalTestFailuresAndRisks proves a run's
// session summary reports the same failing-command and risk-finding counts a
// PR body or Jira comment would render for that same run, because both read
// the same deliverysummary.Build output rather than each deriving their own
// answer.
func TestSessionSourceCountsReflectCanonicalTestFailuresAndRisks(t *testing.T) {
	a := newTestApp(t)
	run := newTestRun(a, "run-test-1")
	if err := a.Workflow.Append(run); err != nil {
		t.Fatalf("Workflow.Append: %v", err)
	}

	bundle, err := evidence.NewBundle(a.Workspace.Root, run.Id, "task-1")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	report := testrun.Report{
		AllPassed: false,
		Results: []testrun.CommandResult{
			{Command: testrun.Command{Name: "go", Args: []string{"test", "./..."}}, ExitCode: 1},
		},
	}
	if err := testrun.WriteBundle(report, bundle); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	ledger, err := evidence.OpenLedger(a.Workspace.Root, run.Id)
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if _, err := evidence.RecordArtifact(ledger, run.Id, "task-1", protocol.EvidenceRecordTypeTestReport, bundle, "tests.json", time.Now().UTC()); err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}
	if err := a.PrReviews.Append(prreview.Record{
		RunId: run.Id, RepoId: "repo-a", PullRequestNumber: 1,
		Findings: []protocol.ReviewFinding{
			{Id: "f1", Severity: protocol.ReviewFindingSeverityBlocker, Explanation: "unchecked error"},
		},
	}); err != nil {
		t.Fatalf("PrReviews.Append: %v", err)
	}

	ss := &SessionSource{App: a}
	summaries, err := ss.List(context.Background(), a.Workspace.ID, contract.SessionFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %+v, want 1", summaries)
	}
	got := summaries[0]
	if got.ErrorCount == nil || *got.ErrorCount != 1 {
		t.Fatalf("ErrorCount = %v, want 1 (the failing go test command)", got.ErrorCount)
	}
	if got.WarningCount == nil || *got.WarningCount != 1 {
		t.Fatalf("WarningCount = %v, want 1 (the blocker-severity finding)", got.WarningCount)
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

// TestKnowledgeSourceSearchIndexesNewWritesWithoutManualRebuild guards
// punokawan-obt: a record written to the store must be findable via
// KnowledgeSource.Search immediately, with no explicit search.Rebuild call.
// The panel used to query the raw index directly and returned nothing for a
// never-indexed record; it now routes through App.SearchKnowledge, which
// watermark-gated-rebuilds before querying. Note this test deliberately never
// calls search.Rebuild itself - that omission is the whole point.
func TestKnowledgeSourceSearchIndexesNewWritesWithoutManualRebuild(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	ks := &KnowledgeSource{App: a}
	ctx := context.Background()

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id:     "pkw:requirement/repo-a/webhook-idempotency",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Webhook idempotency guarantee",
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

	results, err := ks.Search(ctx, a.Workspace.ID, search.Request{Query: "webhook idempotency"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected the just-written record to be searchable without a manual reindex (punokawan-obt)")
	}
	var found bool
	for _, r := range results {
		if r.Id == rec.Id {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Search results %+v do not include the just-written record %q", results, rec.Id)
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
	store, err := a.OpenApprovals()
	if err != nil {
		t.Fatalf("OpenApprovals: %v", err)
	}
	if err := store.Append(rec); err != nil {
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

// TestWorkspaceSourceListSkipsGitHealth guards a fix for the overview
// page's git-status cost scaling linearly with project count: List (used
// by the multi-workspace overview aggregate) must not run gitHealth's
// per-repository `git status` shell-out at all, since the overview never
// displays per-repo git state (that only appears on the project detail
// page, served by Get). Proven here by breaking repo-a's git status (its
// directory is removed) and showing List's Availability is unaffected
// while Get still surfaces the resulting git:repo-a failure.
func TestWorkspaceSourceListSkipsGitHealth(t *testing.T) {
	requireBd(t)
	a := newTestApp(t)
	if err := os.RemoveAll(filepath.Join(a.Workspace.Root, "repo-a")); err != nil {
		t.Fatalf("RemoveAll repo-a: %v", err)
	}
	ws := &WorkspaceSource{App: a}

	summaries, err := ws.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("List = %+v, want 1 entry", summaries)
	}
	if summaries[0].Availability != protocol.PanelSourceHealthAvailabilityAvailable {
		t.Fatalf("List Availability = %s, want available (git health must not factor into List)", summaries[0].Availability)
	}

	detail, err := ws.Get(context.Background(), a.Workspace.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	foundBroken := false
	for _, h := range detail.Health {
		if h.Source == "git:repo-a" && h.Availability == protocol.PanelSourceHealthAvailabilityUnavailable {
			foundBroken = true
		}
	}
	if !foundBroken {
		t.Fatalf("Get.Health = %+v, want a failing git:repo-a entry", detail.Health)
	}
}

// TestGitHealthCoversEveryRepoInDeterministicOrder guards against a
// regression now that gitHealth runs one `git status` per repository
// concurrently (a bounded
// worker pool, mirroring List's), so this proves the parallel fan-out still
// reassembles results in the workspace's declared repository order, not
// whatever order the goroutines happened to finish in - repeated across
// several calls, since a race would not necessarily show up on the first
// one.
func TestGitHealthCoversEveryRepoInDeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	repoIDs := []string{"repo-a", "repo-b", "repo-c"}
	for _, id := range repoIDs {
		repoDir := filepath.Join(dir, id)
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		runGit(t, repoDir, "init", "-q", "-b", "main")
		runGit(t, repoDir, "config", "user.email", "test@example.com")
		runGit(t, repoDir, "config", "user.name", "Test User")
		if err := os.WriteFile(filepath.Join(repoDir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
			t.Fatalf("write f.txt: %v", err)
		}
		runGit(t, repoDir, "add", "f.txt")
		runGit(t, repoDir, "commit", "-q", "-m", "init")
	}

	punakawanDir := filepath.Join(dir, ".punakawan")
	if err := os.MkdirAll(punakawanDir, 0o755); err != nil {
		t.Fatalf("mkdir .punakawan: %v", err)
	}
	workspaceYAML := "version: punakawan.workspace/v1\nid: multi-repo\nname: MultiRepo\nrepositories:\n" +
		"  - id: repo-a\n    path: ./repo-a\n" +
		"  - id: repo-b\n    path: ./repo-b\n" +
		"  - id: repo-c\n    path: ./repo-c\n"
	if err := os.WriteFile(filepath.Join(punakawanDir, "workspace.yaml"), []byte(workspaceYAML), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}

	a, err := app.Load(dir)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	for attempt := 0; attempt < 20; attempt++ {
		health := gitHealth(context.Background(), a, time.Now().UTC())
		if len(health) != len(repoIDs) {
			t.Fatalf("attempt %d: gitHealth returned %d entries, want %d", attempt, len(health), len(repoIDs))
		}
		for i, id := range repoIDs {
			want := "git:" + id
			if health[i].Source != want {
				t.Fatalf("attempt %d: health[%d].Source = %q, want %q (order must match Workspace.Repositories)", attempt, i, health[i].Source, want)
			}
			if health[i].Availability != protocol.PanelSourceHealthAvailabilityAvailable {
				t.Fatalf("attempt %d: %s availability = %s, want available", attempt, want, health[i].Availability)
			}
		}
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
