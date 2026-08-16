package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/timing"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func requireBd(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}
}

func requireDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
}

// initBd runs bd init in a's workspace root, for tests that exercise the
// tasks endpoints: newTestApp itself does not initialize a bd project,
// since most server tests have no need for one.
func initBd(t *testing.T, a *app.App) {
	t.Helper()
	res, err := a.Supervisor.Run(context.Background(), tools.Spec{
		Name: "bd",
		Args: []string{"init", "--non-interactive", "--prefix", "test", "--skip-agents", "--skip-hooks", "-q"},
		Dir:  a.Workspace.Root,
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("bd init: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
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
// one git repository, mirroring internal/panel/sources' own copy of this
// helper.
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
	return a
}

func startTestServer(t *testing.T) (*Server, *app.App) {
	t.Helper()
	a := newTestApp(t)
	reg, err := registry.Open()
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	s := New(a, reg, Options{Port: "0"})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Logf("Shutdown: %v", err)
		}
	})
	return s, a
}

func getJSON(t *testing.T, addr, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://%s%s", addr, path))
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("decode body %s: %v", body, err)
		}
	}
	return resp.StatusCode, out
}

func TestServerRejectsNonLoopbackHost(t *testing.T) {
	if _, err := loopbackListener("0.0.0.0", "0"); err == nil {
		t.Fatal("expected an error binding a non-loopback host")
	}
}

func TestServerSystemEndpoint(t *testing.T) {
	s, _ := startTestServer(t)
	status, body := getJSON(t, s.Addr(), "/api/v1/system")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["read_only"] != false {
		t.Fatalf("read_only = %v, want false (mutation session endpoints now exist)", body["read_only"])
	}
	if body["panel_version"] == "" || body["panel_version"] == nil {
		t.Fatalf("panel_version missing: %+v", body)
	}
}

func TestServerWorkspacesEndpoint(t *testing.T) {
	s, a := startTestServer(t)
	status, body := getJSON(t, s.Addr(), "/api/v1/workspaces")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want 1", items)
	}
	first, _ := items[0].(map[string]any)
	if first["id"] != a.Workspace.ID {
		t.Fatalf("items[0].id = %v, want %q", first["id"], a.Workspace.ID)
	}
}

func TestServerWorkspaceDetailEndpoint(t *testing.T) {
	s, a := startTestServer(t)
	status, body := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["id"] != a.Workspace.ID {
		t.Fatalf("id = %v, want %q", body["id"], a.Workspace.ID)
	}
	if _, ok := body["health"]; !ok {
		t.Fatalf("expected a health field: %+v", body)
	}
}

func TestServerWorkspaceDetailUnknownIDReturns404(t *testing.T) {
	s, _ := startTestServer(t)
	status, _ := getJSON(t, s.Addr(), "/api/v1/workspaces/no-such-workspace")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestServerOverviewEndpoint(t *testing.T) {
	s, _ := startTestServer(t)
	status, body := getJSON(t, s.Addr(), "/api/v1/overview")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, ok := body["workspace_health"]; !ok {
		t.Fatalf("expected a workspace_health field: %+v", body)
	}
}

func TestServerRejectsUnexpectedHostHeader(t *testing.T) {
	s, _ := startTestServer(t)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/v1/system", s.Addr()), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Host = "evil.example.com"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestServerRejectsCrossOrigin(t *testing.T) {
	s, _ := startTestServer(t)
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/api/v1/system", s.Addr()), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Origin", "http://evil.example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestServerSecurityHeadersPresent(t *testing.T) {
	s, _ := startTestServer(t)
	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/system", s.Addr()))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()

	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "Cross-Origin-Resource-Policy"} {
		if resp.Header.Get(header) == "" {
			t.Fatalf("missing security header %s", header)
		}
	}
}

func TestServerStaticFallbackServesIndexForUnknownPath(t *testing.T) {
	s, _ := startTestServer(t)
	resp, err := http.Get(fmt.Sprintf("http://%s/some/unknown/route", s.Addr()))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Punakawan Panel") {
		t.Fatalf("body = %s, want it to contain the panel's index.html", body)
	}
}

func TestServerSessionsEndpoints(t *testing.T) {
	s, a := startTestServer(t)

	run := workflow.New("run-test-1", a.Workspace.ID, protocol.WorkflowRunWorkflowNameFeatureDelivery, time.Now().UTC())
	if err := a.Workflow.Append(run); err != nil {
		t.Fatalf("Workflow.Append: %v", err)
	}

	status, body := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/sessions")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want 1", items)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/sessions/run-test-1")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["id"] != "run-test-1" {
		t.Fatalf("id = %v, want run-test-1", body["id"])
	}
}

func TestServerTasksEndpoints(t *testing.T) {
	requireBd(t)
	s, a := startTestServer(t)
	initBd(t, a)

	res, err := a.Supervisor.Run(context.Background(), tools.Spec{
		Name: "bd",
		Args: []string{"create", "--json", "--title=first task", "--type=task"},
		Dir:  a.Workspace.Root,
	})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("bd create: err=%v exit=%d stderr=%s", err, res.ExitCode, res.Stderr)
	}
	var created struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(res.Stdout, &created); err != nil {
		t.Fatalf("decode bd create output: %v", err)
	}

	status, body := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/tasks")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want 1", items)
	}
	first, _ := items[0].(map[string]any)
	if first["board_status"] != "ready" {
		t.Fatalf("board_status = %v, want ready", first["board_status"])
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/tasks/"+created.Id)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["id"] != created.Id {
		t.Fatalf("id = %v, want %v", body["id"], created.Id)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/task-graph")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	nodes, _ := body["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("nodes = %+v, want 1", nodes)
	}
}

func TestServerTasksEndpointUnknownTaskReturns404(t *testing.T) {
	requireBd(t)
	s, a := startTestServer(t)
	initBd(t, a)

	status, _ := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/tasks/no-such-task")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestServerKnowledgeEndpoints(t *testing.T) {
	requireDolt(t)
	s, a := startTestServer(t)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id:         "pkw:requirement/repo-a/refund-sla",
		Type:       protocol.KnowledgeRecordTypeRequirement,
		Title:      "Refund SLA policy",
		Source:     protocol.KnowledgeRecordSource{Provider: "manual", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateVerified, VerifiedBy: []string{"test"}},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	status, body := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/knowledge")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want 1", items)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/knowledge/"+rec.Id)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["id"] != rec.Id {
		t.Fatalf("id = %v, want %v", body["id"], rec.Id)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/knowledge/"+rec.Id+"/relations")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, ok := body["items"]; !ok {
		t.Fatalf("expected an items field: %+v", body)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/knowledge/"+rec.Id+"/history")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ = body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("history items = %+v, want 1 put event", items)
	}
}

func TestServerKnowledgeHandlerUnknownIDReturns404(t *testing.T) {
	requireDolt(t)
	s, a := startTestServer(t)
	status, _ := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/knowledge/no-such-id")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestServerGlobalSearchEndpoint(t *testing.T) {
	requireDolt(t)
	s, a := startTestServer(t)

	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id:         "pkw:requirement/repo-a/refund-sla",
		Type:       protocol.KnowledgeRecordTypeRequirement,
		Title:      "Refund SLA policy",
		Source:     protocol.KnowledgeRecordSource{Provider: "manual", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
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

	status, body := getJSON(t, s.Addr(), "/api/v1/search?q=refund+SLA")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ := body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("expected at least one fused global search result")
	}
}

func TestServerEvidenceEndpoints(t *testing.T) {
	s, a := startTestServer(t)

	run := workflow.New("run-ev-1", a.Workspace.ID, protocol.WorkflowRunWorkflowNameFeatureDelivery, time.Now().UTC())
	if err := a.Workflow.Append(run); err != nil {
		t.Fatalf("Workflow.Append: %v", err)
	}
	dir := filepath.Join(a.Workspace.Root, ".punakawan", "evidence", "run-ev-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "build.log")
	// awsKeyLooking is concatenated, not a contiguous literal, so this
	// file's raw text doesn't contain a string shaped like a real AWS
	// access key id - GitHub's push protection blocks any push whose diff
	// contains that shape, real or not.
	awsKeyLooking := "AKIA" + "ABCDEFGHIJKLMNOP"
	content := "build ok\nAWS_ACCESS_KEY_ID=" + awsKeyLooking + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write evidence file: %v", err)
	}
	ledger, err := evidence.OpenLedger(a.Workspace.Root, "run-ev-1")
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}
	if err := ledger.Append(protocol.EvidenceRecord{
		Id:        "ev-1",
		RunId:     "run-ev-1",
		Type:      protocol.EvidenceRecordTypeCommandOutput,
		Path:      &path,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("ledger.Append: %v", err)
	}

	status, body := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/sessions/run-ev-1/evidence")
	if status != http.StatusOK {
		t.Fatalf("list: status = %d, want 200", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list items = %+v, want 1", items)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/evidence/ev-1")
	if status != http.StatusOK {
		t.Fatalf("get: status = %d, want 200", status)
	}
	if body["id"] != "ev-1" {
		t.Fatalf("get id = %v, want ev-1", body["id"])
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/evidence/ev-1/preview")
	if status != http.StatusOK {
		t.Fatalf("preview: status = %d, want 200", status)
	}
	text, _ := body["text"].(string)
	if strings.Contains(text, awsKeyLooking) {
		t.Fatalf("preview text = %q, secret was not redacted before reaching the API response", text)
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("preview text = %q, want a [REDACTED] marker", text)
	}
}

func TestServerEvidencePreviewRejectsPathOutsideEvidenceRoot(t *testing.T) {
	s, a := startTestServer(t)

	run := workflow.New("run-ev-2", a.Workspace.ID, protocol.WorkflowRunWorkflowNameFeatureDelivery, time.Now().UTC())
	if err := a.Workflow.Append(run); err != nil {
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

	status, _ := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/evidence/ev-escape/preview")
	if status == http.StatusOK {
		t.Fatal("preview: want a non-200 status for a path outside the evidence directory")
	}
}

func TestServerApprovalsEndpoint(t *testing.T) {
	s, a := startTestServer(t)

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

	status, body := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/approvals")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want 1", items)
	}
	first, _ := items[0].(map[string]any)
	if first["approve_command"] == nil || first["approve_command"] == "" {
		t.Fatalf("approve_command = %v, want a non-empty CLI hint", first["approve_command"])
	}
}

func TestServerSessionsEndpointUnknownSessionReturns404(t *testing.T) {
	s, a := startTestServer(t)
	status, _ := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/sessions/no-such-run")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

// TestServerEventsEndpointStreamsSystemReadyOnConnect proves a fresh SSE
// connection (no Last-Event-ID) always receives its own system.ready
// frame immediately, per §12 - the frontend's connection indicator relies
// on this, so it must not depend on catching a global startup event that
// existed before the client subscribed.
func TestServerEventsEndpointStreamsSystemReadyOnConnect(t *testing.T) {
	s, _ := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/api/v1/events", s.Addr()), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	buf := make([]byte, 4096)
	n, err := resp.Body.Read(buf)
	if err != nil && n == 0 {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "system.ready") {
		t.Fatalf("first SSE frame = %q, want it to contain system.ready", buf[:n])
	}
}

// TestLoggingMiddlewareLabelsStreamingResponsesSeparately guards against an
// SSE handler blocking until the client disconnects: logging it under the
// same duration_ms field as ordinary requests made a multi-minute idle
// connection read as a multi-minute processing stall.
func TestLoggingMiddlewareLabelsStreamingResponsesSeparately(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	sseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()
	loggingMiddleware(logger, sseHandler).ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log line: %v (raw: %s)", err, buf.String())
	}
	if entry["msg"] != "panel stream closed" {
		t.Fatalf("msg = %v, want %q", entry["msg"], "panel stream closed")
	}
	if _, ok := entry["connection_duration_ms"]; !ok {
		t.Fatalf("log entry missing connection_duration_ms: %+v", entry)
	}
	if _, ok := entry["duration_ms"]; ok {
		t.Fatalf("log entry must not carry duration_ms for a streaming response: %+v", entry)
	}

	buf.Reset()
	plainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req = httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec = httptest.NewRecorder()
	loggingMiddleware(logger, plainHandler).ServeHTTP(rec, req)

	entry = nil
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log line: %v (raw: %s)", err, buf.String())
	}
	if entry["msg"] != "panel request" {
		t.Fatalf("msg = %v, want %q", entry["msg"], "panel request")
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Fatalf("log entry missing duration_ms: %+v", entry)
	}
	if _, ok := entry["connection_duration_ms"]; ok {
		t.Fatalf("ordinary request must not carry connection_duration_ms: %+v", entry)
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	a := newTestApp(t)
	reg, err := registry.Open()
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	s := New(a, reg, Options{Port: "0"})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if _, err := http.Get(fmt.Sprintf("http://%s/api/v1/system", s.Addr())); err == nil {
		t.Fatal("expected the connection to be refused after shutdown")
	}
}

// exchangeSession trades s's bootstrap URL for a live session, returning
// an *http.Client whose cookie jar now carries the session cookie and the
// CSRF token that must accompany every mutating request.
func exchangeSession(t *testing.T, s *Server) (*http.Client, string) {
	t.Helper()
	u, err := url.Parse(s.BootstrapURL())
	if err != nil {
		t.Fatalf("parse BootstrapURL: %v", err)
	}
	bootstrapToken := u.Query().Get("bootstrap")
	if bootstrapToken == "" {
		t.Fatalf("BootstrapURL %q has no bootstrap query param", s.BootstrapURL())
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}

	body, _ := json.Marshal(map[string]string{"bootstrap_token": bootstrapToken})
	resp, err := client.Post(fmt.Sprintf("http://%s/api/v1/session/exchange", s.Addr()), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("exchange status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode exchange response: %v", err)
	}
	return client, out.CSRFToken
}

func TestServerSessionExchangeGrantsAWorkingSession(t *testing.T) {
	s, a := startTestServer(t)
	plans := &artifact.PlanStore{WorkspaceRoot: a.Workspace.Root}
	if _, err := plans.CreateVersion("plan-panel", a.Workspace.ID, []byte("# Plan\n\nBody.\n"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	client, csrfToken := exchangeSession(t, s)

	createBody, _ := json.Marshal(map[string]string{"title": "Panel review"})
	resp, err := client.Post(fmt.Sprintf("http://%s/api/v1/artifacts/plan/plan-panel/reviews", s.Addr()), "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status without CSRF header = %d, want 403", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/v1/artifacts/plan/plan-panel/reviews", s.Addr()), bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Csrf-Token", csrfToken)
	req.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatalf("create review with CSRF: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("status with CSRF header = %d, want 201: %s", resp2.StatusCode, body)
	}
}

func TestServerRejectsMutationWithNoSessionCookie(t *testing.T) {
	s, _ := startTestServer(t)
	body, _ := json.Marshal(map[string]string{"title": "x"})
	resp, err := http.Post(fmt.Sprintf("http://%s/api/v1/artifacts/plan/plan-panel/reviews", s.Addr()), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServerRejectsFailReviewWithNoSessionCookie(t *testing.T) {
	s, _ := startTestServer(t)
	resp, err := http.Post(fmt.Sprintf("http://%s/api/v1/reviews/no-such-review/fail", s.Addr()), "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestServerSessionIsInvalidatedOnShutdown(t *testing.T) {
	a := newTestApp(t)
	reg, err := registry.Open()
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	s := New(a, reg, Options{Port: "0"})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	client, _ := exchangeSession(t, s)
	apiURL, err := url.Parse(fmt.Sprintf("http://%s/", s.Addr()))
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	cookies := client.Jar.Cookies(apiURL)
	if len(cookies) != 1 {
		t.Fatalf("cookies = %+v, want the one session cookie the exchange just set", cookies)
	}
	sessionID := cookies[0].Value
	if !s.sessions.ValidSession(sessionID) {
		t.Fatal("ValidSession = false right after exchange, want true")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if s.sessions.ValidSession(sessionID) {
		t.Fatal("ValidSession = true after Shutdown, want the session invalidated")
	}
}

func TestServerSubmitDispatchesABDTaskGraph(t *testing.T) {
	requireBd(t)
	requireDolt(t)

	s, a := startTestServer(t)
	initBd(t, a)

	plans := &artifact.PlanStore{WorkspaceRoot: a.Workspace.Root}
	if _, err := plans.CreateVersion("plan-panel", a.Workspace.ID, []byte("# Plan\n\nBody.\n"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	client, csrfToken := exchangeSession(t, s)
	doWithCSRF := func(method, path string, body []byte) *http.Response {
		req, err := http.NewRequest(method, fmt.Sprintf("http://%s%s", s.Addr(), path), bytes.NewReader(body))
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("X-Csrf-Token", csrfToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}

	createBody, _ := json.Marshal(map[string]string{"title": "Panel review"})
	createResp := doWithCSRF(http.MethodPost, "/api/v1/artifacts/plan/plan-panel/reviews", createBody)
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("create review status = %d: %s", createResp.StatusCode, body)
	}
	var review protocol.ArtifactReview
	if err := json.NewDecoder(createResp.Body).Decode(&review); err != nil {
		t.Fatalf("decode review: %v", err)
	}

	submitResp := doWithCSRF(http.MethodPost, "/api/v1/reviews/"+review.Metadata.Id+"/submit", nil)
	defer submitResp.Body.Close()
	if submitResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(submitResp.Body)
		t.Fatalf("submit status = %d, want 201: %s", submitResp.StatusCode, body)
	}
	var submitted struct {
		Run struct {
			RunID string `json:"run_id"`
		} `json:"run"`
	}
	if err := json.NewDecoder(submitResp.Body).Decode(&submitted); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if submitted.Run.RunID == "" {
		t.Fatal("submit response has no run_id")
	}

	res, err := a.Supervisor.Run(context.Background(), tools.Spec{Name: "bd", Args: []string{"show", submitted.Run.RunID}, Dir: a.Workspace.Root})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("bd show %s: err=%v exit=%d stderr=%s", submitted.Run.RunID, err, res.ExitCode, res.Stderr)
	}
}

// probingHandler records one probe named "handler_work" (completing its timed
// section before writing, as panel handlers record sub-reads before writeJSON)
// then writes a JSON body via WriteHeader+Write - the case timingMiddleware's
// just-in-time header injection is built for.
func probingHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stop := timing.Probe(r.Context(), "handler_work")
		stop()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func TestTimingMiddlewareSetsServerTimingWhenEnabled(t *testing.T) {
	h := timingMiddleware(true, probingHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := rec.Header().Get("Server-Timing")
	if !strings.HasPrefix(got, "handler_work;dur=") {
		t.Fatalf("Server-Timing = %q, want handler_work;dur= prefix", got)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Fatalf("body = %q, want the handler's JSON (header injection must not corrupt the body)", rec.Body.String())
	}
}

func TestTimingMiddlewareAbsentWhenDisabled(t *testing.T) {
	h := timingMiddleware(false, probingHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)

	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Server-Timing"); got != "" {
		t.Fatalf("Server-Timing = %q, want empty when disabled", got)
	}
	// The probe in the handler must have been a harmless no-op.
	if _, ok := timing.FromContext(req.Context()); ok {
		t.Fatal("no collector should be attached when timing is disabled")
	}
}

// TestServerServerTimingEndToEnd confirms the Options flag actually wires the
// header through the real middleware chain (security -> timing -> logging).
func TestServerServerTimingEndToEnd(t *testing.T) {
	a := newTestApp(t)
	reg, err := registry.Open()
	if err != nil {
		t.Fatalf("registry.Open: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	s := New(a, reg, Options{Port: "0", ServerTiming: true})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Logf("Shutdown: %v", err)
		}
	})

	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/overview", s.Addr()))
	if err != nil {
		t.Fatalf("GET /api/v1/overview: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got := resp.Header.Get("Server-Timing")
	if !strings.Contains(got, "overview_aggregate;dur=") {
		t.Fatalf("Server-Timing = %q, want it to include overview_aggregate;dur=", got)
	}
}
