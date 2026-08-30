package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistEnvValuesMergesWithoutDisturbingUnrelatedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	seed := "# a comment\nOTHER_TOKEN=keep-me\nGITHUB_TOKEN=stale\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := persistEnvValues(path, map[string]string{"GITHUB_TOKEN": "fresh"}); err != nil {
		t.Fatalf("persistEnvValues: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# a comment") {
		t.Fatalf("comment was dropped: %q", content)
	}
	if !strings.Contains(content, "OTHER_TOKEN=keep-me") {
		t.Fatalf("unrelated value was dropped: %q", content)
	}
	if strings.Contains(content, "stale") {
		t.Fatalf("stale value was not replaced: %q", content)
	}
	if !strings.Contains(content, "GITHUB_TOKEN=fresh") {
		t.Fatalf("new value was not written: %q", content)
	}

	parsed, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("parseEnvFile: %v", err)
	}
	if parsed["GITHUB_TOKEN"] != "fresh" || parsed["OTHER_TOKEN"] != "keep-me" {
		t.Fatalf("parsed = %+v, want both values intact", parsed)
	}
}

func TestPersistEnvValuesAppendsNewKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")

	if err := persistEnvValues(path, map[string]string{"ATLASSIAN_HOST": "team.atlassian.net"}); err != nil {
		t.Fatalf("persistEnvValues: %v", err)
	}
	if err := persistEnvValues(path, map[string]string{"ATLASSIAN_API_TOKEN": "abc123"}); err != nil {
		t.Fatalf("persistEnvValues (second key): %v", err)
	}

	parsed, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("parseEnvFile: %v", err)
	}
	if parsed["ATLASSIAN_HOST"] != "team.atlassian.net" || parsed["ATLASSIAN_API_TOKEN"] != "abc123" {
		t.Fatalf("parsed = %+v, want both keys present", parsed)
	}
}

func TestPersistEnvValuesRoundTripsValuesNeedingQuoting(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	tricky := "value with spaces and a ' quote"

	if err := persistEnvValues(path, map[string]string{"ATLASSIAN_EMAIL": tricky}); err != nil {
		t.Fatalf("persistEnvValues: %v", err)
	}
	parsed, err := parseEnvFile(path)
	if err != nil {
		t.Fatalf("parseEnvFile: %v", err)
	}
	if parsed["ATLASSIAN_EMAIL"] != tricky {
		t.Fatalf("round-tripped value = %q, want %q", parsed["ATLASSIAN_EMAIL"], tricky)
	}
}

func TestPersistEnvValuesFilePermissionsAreOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := persistEnvValues(path, map[string]string{"GITHUB_TOKEN": "abc"}); err != nil {
		t.Fatalf("persistEnvValues: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %v, want 0600", info.Mode().Perm())
	}
}

func TestResolveOrPromptPrefersProcessEnvAndNeverPromptsWhenNonInteractive(t *testing.T) {
	t.Setenv("PUNAKAWAN_TEST_CRED", "from-env")
	var out strings.Builder
	got := resolveOrPrompt(strings.NewReader(""), &out, map[string]string{"PUNAKAWAN_TEST_CRED": "from-file"}, "PUNAKAWAN_TEST_CRED", "prompt", true)
	if got != "from-env" {
		t.Fatalf("resolveOrPrompt = %q, want the live process value", got)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no prompt to be printed when a value is already resolved, got %q", out.String())
	}
}

func TestResolveOrPromptFallsBackToExistingFileValueWithoutPrompting(t *testing.T) {
	var out strings.Builder
	got := resolveOrPrompt(strings.NewReader(""), &out, map[string]string{"PUNAKAWAN_TEST_CRED_2": "from-file"}, "PUNAKAWAN_TEST_CRED_2", "prompt", false)
	if got != "from-file" {
		t.Fatalf("resolveOrPrompt = %q, want the durable file value", got)
	}
}

func TestResolveOrPromptReturnsEmptyWhenNothingAvailableAndNotInteractive(t *testing.T) {
	var out strings.Builder
	got := resolveOrPrompt(strings.NewReader(""), &out, map[string]string{}, "PUNAKAWAN_TEST_CRED_MISSING", "prompt", true)
	if got != "" {
		t.Fatalf("resolveOrPrompt = %q, want empty when nothing is available and stdin is not a terminal", got)
	}
}

// TestSetupAtlassianCredentialsNeverLeaksTheTokenOnAFailedValidation
// exercises the credential-validation path against a fake Atlassian site
// under a scheme checkAtlassianConnectivity does not speak (plain HTTP;
// it always dials https), guaranteeing a failure, and confirms the
// credential value never appears in anything the command wrote to its
// output writer or returned error - the redaction guarantee this
// command's whole design depends on. persistEnvValues' own round-trip
// tests above cover the on-success persistence path.
func TestSetupAtlassianCredentialsNeverLeaksTheTokenOnAFailedValidation(t *testing.T) {
	const secretToken = "sekret-should-never-be-printed"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	envPath := filepath.Join(t.TempDir(), ".env")
	existing := map[string]string{"ATLASSIAN_HOST": host, "ATLASSIAN_API_TOKEN": secretToken, "ATLASSIAN_EMAIL": "agent@example.com"}
	var out strings.Builder

	err := setupAtlassianCredentials(context.Background(), strings.NewReader(""), &out, envPath, existing)
	// checkAtlassianConnectivity always dials https://<host>; an
	// httptest.Server only serves plain HTTP, so this specific call is
	// expected to fail at the network layer - the point of this test is
	// that whatever setupAtlassianCredentials printed along the way never
	// contains the token, not that the httptest server's scheme matches.
	if err == nil {
		t.Fatal("expected an error dialing a plain-HTTP test server over the hardcoded https scheme")
	}
	if strings.Contains(out.String(), secretToken) {
		t.Fatalf("output leaked the credential value: %q", out.String())
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("returned error leaked the credential value: %v", err)
	}
}
