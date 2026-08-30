package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
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
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/timing"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func requireDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
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

	status, body := getJSON(t, s.Addr(), "/api/v1/projects/"+a.Workspace.ID+"/knowledge")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %+v, want 1", items)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/projects/"+a.Workspace.ID+"/knowledge/"+rec.Id)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["id"] != rec.Id {
		t.Fatalf("id = %v, want %v", body["id"], rec.Id)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/projects/"+a.Workspace.ID+"/knowledge/"+rec.Id+"/relations")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if _, ok := body["items"]; !ok {
		t.Fatalf("expected an items field: %+v", body)
	}

	status, body = getJSON(t, s.Addr(), "/api/v1/projects/"+a.Workspace.ID+"/knowledge/"+rec.Id+"/history")
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
	status, _ := getJSON(t, s.Addr(), "/api/v1/projects/"+a.Workspace.ID+"/knowledge/no-such-id")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
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

// TestServerShutdownDrainsInFlightRequest proves Shutdown's first step (stop
// accepting HTTP, drain active HTTP requests) actually waits for a request
// whose handler is still running to finish, rather than cutting it off: a
// raw connection sends only part of a POST body - so the handler is blocked
// inside its JSON decode, mid-request - before Shutdown is called; Shutdown
// must not return until the rest of the body is sent and the handler
// completes, and the client must still receive a well-formed HTTP response
// rather than a reset connection.
func TestServerShutdownDrainsInFlightRequest(t *testing.T) {
	s, _ := startTestServer(t)

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	body := `{"bootstrap_token":"deliberately-invalid-token"}`
	half := len(body) / 2
	request := fmt.Sprintf(
		"POST /api/v1/session/exchange HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		s.Addr(), len(body), body[:half],
	)
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatalf("write partial request: %v", err)
	}

	// Give the server a moment to accept the connection and start blocking in
	// the handler's body decode before Shutdown is invoked.
	time.Sleep(50 * time.Millisecond)

	shutdownDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownDone <- s.Shutdown(ctx)
	}()

	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown returned (err=%v) before the in-flight request's body was even fully sent - it did not drain", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: Shutdown is still blocked draining the active connection.
	}

	if _, err := conn.Write([]byte(body[half:])); err != nil {
		t.Fatalf("write remaining request body: %v", err)
	}

	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("expected a well-formed HTTP response after draining, got: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (invalid bootstrap token, but still a real handled response)", resp.StatusCode)
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

// TestWorkflowInvokeRouteMatchesMCPOnRolesShapedDefinition: invoking a
// Roles-shaped definition through the panel's own
// /workflows/{id}/invoke route must produce a real internal/delivery
// orchestration (with a plan_id attached), the same way
// invoke_workflow_definition already does over MCP - not a legacy
// internal/workflow run, which is what this route always created before
// this fix (punokawan-pkcd.3).
func TestWorkflowInvokeRouteMatchesMCPOnRolesShapedDefinition(t *testing.T) {
	s, a := startTestServer(t)
	client, csrf := exchangeSession(t, s)

	defStore, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		t.Fatalf("workflowdef.Open: %v", err)
	}
	if _, err := defStore.Save(workflowdef.Definition{
		Version: workflowdef.SchemaVersion,
		ID:      "panel-hotfix-delivery",
		Name:    "Panel Hotfix Delivery",
		Enabled: true,
		Roles: map[string]workflowdef.RoleRestriction{
			"gareng": {Required: false},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	body, _ := json.Marshal(map[string]any{"inputs": map[string]any{"references": []string{"JIRA-9001"}}})
	target := fmt.Sprintf("http://%s/api/v1/projects/%s/workflows/panel-hotfix-delivery/invoke", s.Addr(), a.Workspace.ID)
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Csrf-Token", csrf)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read invoke response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("invoke status = %d, want 200: %s", resp.StatusCode, respBody)
	}
	var out struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		t.Fatalf("decode invoke response: %v", err)
	}
	if out.RunID == "" {
		t.Fatal("invoke returned an empty run_id")
	}

	ctx := context.Background()
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	view, err := delivery.NewStore(db).BuildDeliveryView(ctx, out.RunID)
	if err != nil {
		t.Fatalf("BuildDeliveryView(%s): %v (run_id was not a delivery orchestration)", out.RunID, err)
	}
	if view.Orchestration.WorkflowDefinitionId == nil || *view.Orchestration.WorkflowDefinitionId != "panel-hotfix-delivery" {
		t.Fatalf("expected orchestration.workflow_definition_id = panel-hotfix-delivery, got %v", view.Orchestration.WorkflowDefinitionId)
	}
	if view.PlanID == "" {
		t.Fatal("expected the delivery to reference a plan_id")
	}

	if _, err := a.Workflow.Get(out.RunID); err == nil {
		t.Fatal("expected no legacy workflow run to exist for a Roles-shaped definition")
	}
}

// Deregistering a project is destructive from the panel's point of view, so
// the route must be gated exactly like the metadata mutations. The api-package
// tests call the handler directly and so never exercise the wrapper; this one
// goes through the real mux.
func TestProjectDeleteRouteRequiresSessionAndCSRF(t *testing.T) {
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
		_ = s.Shutdown(ctx)
	})

	target := fmt.Sprintf("http://%s/api/v1/projects/some-project", s.Addr())

	// No session cookie at all.
	req, err := http.NewRequest(http.MethodDelete, target, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete without session: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status without session = %d, want 401", resp.StatusCode)
	}

	// Valid session, but the CSRF header is missing.
	client, csrf := exchangeSession(t, s)
	req, err = http.NewRequest(http.MethodDelete, target, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete without CSRF: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status without CSRF header = %d, want 403", resp.StatusCode)
	}

	// Fully authenticated: the request now reaches the handler, which answers
	// 404 because "some-project" was never registered. Anything but 401/403
	// proves the gate let it through.
	req, err = http.NewRequest(http.MethodDelete, target, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Csrf-Token", csrf)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete with session and CSRF: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status for an unregistered project = %d, want 404", resp.StatusCode)
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
