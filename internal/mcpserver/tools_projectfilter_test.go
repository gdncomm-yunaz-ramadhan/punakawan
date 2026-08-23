package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newProjectTestApp builds a real *app.App whose workspace id is projectID, so
// its knowledge is scoped to that project within the shared SQLite kernel.
// Unlike newTestApp it does NOT set PUNAKAWAN_DATA_DIR: cross-project tests set
// one shared data dir for the whole test so two apps with different project ids
// reach the same kernel, which is exactly what makes a deliberately-named
// sibling project reachable.
func newProjectTestApp(t *testing.T, projectID string) *app.App {
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
	workspaceYAML := "version: punakawan.workspace/v1\nid: " + projectID + "\nname: " + projectID + "\nrepositories:\n  - id: repo-a\n    path: ./repo-a\n"
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

func TestGetKnowledgeRecordsDefaultsToTheCallingProject(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())

	a := newProjectTestApp(t, "proj-caller")
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := putFixtureRecord(t, store, "OWN-1", "own project record", nil)

	var out map[string]any
	callTool(t, cs, "get_knowledge_records", map[string]any{
		"ids": []string{rec.Id},
	}, &out)

	records, _ := out["records"].([]any)
	if len(records) != 1 {
		t.Fatalf("expected 1 record with no project_id given, got %v", out)
	}
}

func TestGetKnowledgeRecordsReachesASiblingProjectWhenNamedExplicitly(t *testing.T) {
	// One shared kernel for both projects, so naming the sibling explicitly can
	// reach it.
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())

	other := newProjectTestApp(t, "proj-other")
	otherStore, err := other.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge (other): %v", err)
	}
	otherRec := putFixtureRecord(t, otherStore, "OTHER-1", "sibling project record", nil)

	caller := newProjectTestApp(t, "proj-caller")
	cs := connect(t, caller)

	// Without project_id, the caller's own project must not see it.
	var out map[string]any
	callTool(t, cs, "get_knowledge_records", map[string]any{
		"ids": []string{otherRec.Id},
	}, &out)
	notFound, _ := out["not_found"].([]any)
	if len(notFound) != 1 {
		t.Fatalf("expected the sibling project's record to be not_found by default, got %v", out)
	}

	// Naming proj-other explicitly must reach it.
	var out2 map[string]any
	callTool(t, cs, "get_knowledge_records", map[string]any{
		"ids":        []string{otherRec.Id},
		"project_id": "proj-other",
	}, &out2)
	records, _ := out2["records"].([]any)
	if len(records) != 1 {
		t.Fatalf("expected 1 record when project_id=proj-other is named explicitly, got %v", out2)
	}
}

func TestSearchKnowledgeDefaultsToTheCallingProject(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())

	a := newProjectTestApp(t, "proj-caller")
	cs := connect(t, a)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id: "pkw:req/fixture/SEARCH-OWN", Type: protocol.KnowledgeRecordTypeRequirement, Status: "active", Title: "distinctive-marker-heron",
		Source:     protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var out map[string]any
	callTool(t, cs, "search_knowledge", map[string]any{
		"query": "distinctive-marker-heron",
	}, &out)
	results, _ := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected 1 result from the own-project BM25 path, got %v", out)
	}
}

func TestSearchKnowledgeCrossProjectFallsBackToASubstringScan(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())

	other := newProjectTestApp(t, "proj-other")
	otherStore, err := other.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge (other): %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id: "pkw:req/fixture/SEARCH-OTHER", Type: protocol.KnowledgeRecordTypeRequirement, Status: "active", Title: "unique-marker-osprey",
		Source:     protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := otherStore.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	caller := newProjectTestApp(t, "proj-caller")
	cs := connect(t, caller)

	var out map[string]any
	callTool(t, cs, "search_knowledge", map[string]any{
		"query":      "unique-marker-osprey",
		"project_id": "proj-other",
	}, &out)
	results, _ := out["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected 1 cross-project result, got %v", out)
	}
	first, _ := results[0].(map[string]any)
	match, _ := first["match"].(map[string]any)
	if kind, _ := match["kind"].(string); kind != "cross_project_scan" {
		t.Fatalf("expected match.kind=cross_project_scan, got %v", match)
	}
}
