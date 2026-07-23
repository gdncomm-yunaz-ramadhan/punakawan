package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/capsule"
	"github.com/ygrip/punakawan/internal/panel/registry"
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
	if body["read_only"] != true {
		t.Fatalf("read_only = %v, want true", body["read_only"])
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
