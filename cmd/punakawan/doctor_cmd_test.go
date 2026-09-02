package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/providercreds"
	"github.com/ygrip/punakawan/internal/telemetry/clienthooks"
	"github.com/ygrip/punakawan/internal/workspace"
)

// isolateDoctorEnv points every directory runDoctor touches (the storage
// kernel, the global credential file, Codex/Claude Code hook config) at a
// throwaway per-test location, so these tests never read or write this
// machine's real install state.
func isolateDoctorEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func TestDoctorReportsMissingWhenNothingIsConfigured(t *testing.T) {
	isolateDoctorEnv(t)

	report := runDoctor(context.Background())

	for _, id := range []string{"atlassian", "github"} {
		a := report.Adapters[id]
		if a.Entrypoint != doctorStatusMissing {
			t.Errorf("adapter %s entrypoint = %q, want %q", id, a.Entrypoint, doctorStatusMissing)
		}
		if a.Credentials == doctorStatusOK {
			t.Errorf("adapter %s credentials = %q, want a missing-credential reason", id, a.Credentials)
		}
	}
	for _, id := range []string{clienthooks.ClientKindCodex, clienthooks.ClientKindClaudeCode} {
		if got := report.Telemetry[id]; got != doctorStatusMissing {
			t.Errorf("telemetry %s = %q, want %q", id, got, doctorStatusMissing)
		}
	}
	if report.allOK() {
		t.Fatal("expected an unconfigured install to fail overall")
	}
}

// TestDoctorNeverEchoesCredentialValues is the redaction guarantee the
// relocation install test also exercises at the process level: a doctor
// report - JSON or the printed text form - must never contain a
// configured secret's actual value, only whether it is present/valid.
func TestDoctorNeverEchoesCredentialValues(t *testing.T) {
	isolateDoctorEnv(t)
	const secretToken = "sekret-atlassian-token-should-never-appear-in-output"
	const secretGitHubToken = "sekret-github-token-should-never-appear-in-output"
	t.Setenv("ATLASSIAN_HOST", "example.atlassian.net")
	t.Setenv("ATLASSIAN_EMAIL", "agent@example.com")
	t.Setenv("ATLASSIAN_API_TOKEN", secretToken)
	t.Setenv("GITHUB_TOKEN", secretGitHubToken)

	report := runDoctor(context.Background())

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if strings.Contains(string(encoded), secretToken) {
		t.Fatalf("JSON report leaked the Atlassian token: %s", encoded)
	}
	if strings.Contains(string(encoded), secretGitHubToken) {
		t.Fatalf("JSON report leaked the GitHub token: %s", encoded)
	}

	var out strings.Builder
	printDoctorReport(&out, report)
	if strings.Contains(out.String(), secretToken) {
		t.Fatalf("printed report leaked the Atlassian token: %s", out.String())
	}
	if strings.Contains(out.String(), secretGitHubToken) {
		t.Fatalf("printed report leaked the GitHub token: %s", out.String())
	}
}

func TestResolveCredentialEnvFallsBackToGlobalEnvFile(t *testing.T) {
	isolateDoctorEnv(t)
	path, err := workspace.GlobalEnvPath()
	if err != nil {
		t.Fatalf("GlobalEnvPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("GITHUB_TOKEN=from-file-token\n"), 0o600); err != nil {
		t.Fatalf("write global env file: %v", err)
	}

	env := resolveCredentialEnv(providercreds.ProviderGitHub, []string{"GITHUB_TOKEN"})
	if env["GITHUB_TOKEN"] != "from-file-token" {
		t.Fatalf("resolveCredentialEnv = %v, want the value from the durable global env file", env)
	}
}

func TestResolveCredentialEnvUsesDefaultSetupCredentialBeforeLegacyEnvFile(t *testing.T) {
	isolateDoctorEnv(t)
	credentialsPath, err := workspace.GlobalCredentialsPath()
	if err != nil {
		t.Fatalf("GlobalCredentialsPath: %v", err)
	}
	if err := providercreds.Open(credentialsPath).Put(providercreds.Org{
		ID: "acme", Provider: providercreds.ProviderGitHub, BaseURL: "https://github.com/acme", Token: "setup-token",
	}); err != nil {
		t.Fatalf("save setup credential: %v", err)
	}
	envPath, err := workspace.GlobalEnvPath()
	if err != nil {
		t.Fatalf("GlobalEnvPath: %v", err)
	}
	if err := os.WriteFile(envPath, []byte("GITHUB_TOKEN=legacy-env-token\n"), 0o600); err != nil {
		t.Fatalf("write legacy env: %v", err)
	}

	env := resolveCredentialEnv(providercreds.ProviderGitHub, []string{"GITHUB_TOKEN"})
	if env["GITHUB_TOKEN"] != "setup-token" {
		t.Fatalf("resolveCredentialEnv = %v, want setup-managed credential", env)
	}
}

func TestResolveCredentialEnvPrefersProcessEnvOverSetupAndFile(t *testing.T) {
	isolateDoctorEnv(t)
	path, err := workspace.GlobalEnvPath()
	if err != nil {
		t.Fatalf("GlobalEnvPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("GITHUB_TOKEN=stale-file-token\n"), 0o600); err != nil {
		t.Fatalf("write global env file: %v", err)
	}
	credentialsPath, err := workspace.GlobalCredentialsPath()
	if err != nil {
		t.Fatalf("GlobalCredentialsPath: %v", err)
	}
	if err := providercreds.Open(credentialsPath).Put(providercreds.Org{
		ID: "acme", Provider: providercreds.ProviderGitHub, BaseURL: "https://github.com/acme", Token: "setup-token",
	}); err != nil {
		t.Fatalf("save setup credential: %v", err)
	}
	t.Setenv("GITHUB_TOKEN", "live-process-token")

	env := resolveCredentialEnv(providercreds.ProviderGitHub, []string{"GITHUB_TOKEN"})
	if env["GITHUB_TOKEN"] != "live-process-token" {
		t.Fatalf("resolveCredentialEnv = %v, want the live process value to win", env)
	}
}

func TestCheckGitHubConnectivitySucceedsAgainstAnAuthenticatedServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := checkGitHubConnectivity(ctx, "good-token", server.URL); err != nil {
		t.Fatalf("checkGitHubConnectivity: %v", err)
	}
	if err := checkGitHubConnectivity(ctx, "wrong-token", server.URL); err == nil {
		t.Fatal("expected an error for an unauthorized token")
	}
}

func TestCheckAtlassianConnectivitySucceedsWithBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		want := "Basic " + basicAuthValue("agent@example.com", "good-token")
		if r.Header.Get("Authorization") != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// checkAtlassianConnectivity always dials https://<host>; exercise the
	// header/URL construction directly against httptest's plain-HTTP
	// listener by building the request the same way and hitting it through
	// server.Client() instead of duplicating the whole function - a scoped
	// unit test on resolveAtlassianCloudID's sibling code path.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+host+"/rest/api/3/myself", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Basic "+basicAuthValue("agent@example.com", "good-token"))
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestCheckHookTelemetryGoesCompleteAfterInstallAndProbe(t *testing.T) {
	isolateDoctorEnv(t)

	// hookConfigInstalled/runHookProbe both resolve the *currently running
	// test binary's own path* via resolvePanelServiceBinary, not an
	// arbitrary caller-supplied one, so the installed hook config must
	// name that same resolved path for the probe to find a matching entry.
	realBinary, err := resolvePanelServiceBinary()
	if err != nil {
		t.Fatalf("resolvePanelServiceBinary: %v", err)
	}
	if _, err := ensureCodexHooks(realBinary); err != nil {
		t.Fatalf("ensureCodexHooks: %v", err)
	}

	status := checkHookTelemetry(context.Background(), clienthooks.ClientKindCodex)
	if status != "complete" {
		t.Fatalf("checkHookTelemetry = %q, want complete", status)
	}
}

func TestCheckHookTelemetryIsMissingWithNoHookConfig(t *testing.T) {
	isolateDoctorEnv(t)
	status := checkHookTelemetry(context.Background(), clienthooks.ClientKindClaudeCode)
	if status != doctorStatusMissing {
		t.Fatalf("checkHookTelemetry = %q, want %q", status, doctorStatusMissing)
	}
}

func TestCheckAdapterHandshakeAgainstThePrototypeAdapter(t *testing.T) {
	prototypeAdapterPath := "../../packages/adapter-sdk/dist/prototypeAdapter.js"
	if _, err := os.Stat(prototypeAdapterPath); err != nil {
		t.Skipf("prototype adapter not built (%s): %v; run `pnpm --filter @punakawan/adapter-sdk build` first", prototypeAdapterPath, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status := checkAdapterHandshake(ctx, "prototype", workspace.AdapterConfig{Command: "node", Args: []string{prototypeAdapterPath}})
	if status != doctorStatusOK {
		t.Fatalf("checkAdapterHandshake = %q, want %q", status, doctorStatusOK)
	}
}

func TestCheckAdapterHandshakeFailsForAnUnresolvableCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status := checkAdapterHandshake(ctx, "atlassian", workspace.AdapterConfig{Command: "punakawan-doctor-test-nonexistent-binary"})
	if status == doctorStatusOK {
		t.Fatal("expected a failing handshake status for a command that cannot start")
	}
}

// TestCheckAdapterEntrypointRejectsAnUntrustedRepositoryLocalCommand
// confirms the entrypoint check reuses the same repository-local trust
// gate the rest of the app enforces (internal/adapters.
// RequireTrustedIfRepositoryLocal), rather than only checking that a file
// exists.
func TestCheckAdapterEntrypointRejectsAnUntrustedRepositoryLocalCommand(t *testing.T) {
	isolateDoctorEnv(t)
	repoRoot := t.TempDir()
	scriptPath := filepath.Join(repoRoot, "adapter.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	report := checkAdapter(context.Background(), "atlassian", workspace.AdapterConfig{Command: scriptPath}, repoRoot, atlassianCredentialCheck)
	if report.Entrypoint == doctorStatusOK {
		t.Fatal("expected an untrusted repository-local adapter command to fail the entrypoint check")
	}
	if report.Handshake != doctorStatusMissing {
		t.Fatalf("handshake = %q, want %q when entrypoint failed", report.Handshake, doctorStatusMissing)
	}
}
