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
	"github.com/ygrip/punakawan/internal/panel/registry"
)

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
