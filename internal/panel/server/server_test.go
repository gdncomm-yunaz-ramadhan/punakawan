package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/capsule"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/panel/registry"
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
	reg, err := registry.OpenAt(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatalf("registry.OpenAt: %v", err)
	}

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

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/sessions/run-test-1/timeline")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, ok := body["items"]; !ok {
		t.Fatalf("expected an items field: %+v", body)
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
	nodes, _ := body["Nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("Nodes = %+v, want 1", nodes)
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
	if err := a.Approvals.Append(rec); err != nil {
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

func TestServerCapsulesEndpoint(t *testing.T) {
	s, a := startTestServer(t)

	c := protocol.ContextCapsule{
		Id:               "cap-1",
		TaskId:           "bd-task-1",
		CreatedAt:        time.Now().UTC(),
		Role:             protocol.ContextCapsuleRolePetruk,
		Objective:        "implement the feature",
		AllowedTools:     []string{},
		ForbiddenActions: []string{},
	}
	digest, err := capsule.Digest(c)
	if err != nil {
		t.Fatalf("capsule.Digest: %v", err)
	}
	c.Digest = digest
	if err := a.Capsules.Put(c); err != nil {
		t.Fatalf("Capsules.Put: %v", err)
	}

	status, body := getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/capsules?task_id=bd-task-1")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want 1", items)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/workspaces/"+a.Workspace.ID+"/capsules?task_id=no-such-task")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ = body["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("items = %+v, want 0 for an unrelated task id", items)
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

func TestServerGracefulShutdown(t *testing.T) {
	a := newTestApp(t)
	reg, err := registry.OpenAt(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatalf("registry.OpenAt: %v", err)
	}
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

func TestServerSessionIsInvalidatedOnShutdown(t *testing.T) {
	a := newTestApp(t)
	reg, err := registry.OpenAt(filepath.Join(t.TempDir(), "workspaces.yaml"))
	if err != nil {
		t.Fatalf("registry.OpenAt: %v", err)
	}
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
