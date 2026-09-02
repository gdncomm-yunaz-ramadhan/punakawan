package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/providercreds"
)

func TestConnectorsReportsEveryAdapterAndItsOrganisations(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "atlassian", "dist", "run.js")
	if err := os.MkdirAll(filepath.Dir(entrypoint), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entrypoint, []byte("//"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := providercreds.Open(filepath.Join(dir, "credentials.yaml"))
	for _, org := range []providercreds.Org{
		{ID: "gdncomm", Provider: providercreds.ProviderJira, BaseURL: "https://gdncomm.atlassian.net", Email: "someone@example.test", Token: "secret-jira-token", LastVerifiedAt: time.Now().UTC()},
		{ID: "acme", Provider: providercreds.ProviderJira, BaseURL: "https://acme.atlassian.net", Email: "other@example.test", Token: "another-secret"},
		{ID: "widgets", Provider: providercreds.ProviderGitHub, BaseURL: "https://github.com/widgets", Token: "gh-secret"},
	} {
		if err := store.Put(org); err != nil {
			t.Fatalf("seed %s: %v", org.ID, err)
		}
	}

	specs := func() map[string]adapters.AdapterSpec {
		return map[string]adapters.AdapterSpec{
			"atlassian": {Command: "node", Args: []string{entrypoint}, EnvPassthrough: []string{"ATLASSIAN_API_TOKEN"}},
			// Configured but never deployed, which must read as installed:false
			// rather than as absent.
			"docling": {Command: "node", Args: []string{filepath.Join(dir, "missing", "run.js")}},
		}
	}

	rec := httptest.NewRecorder()
	ConnectorsHandler(specs, store)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/connectors", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	// A token must never leave this endpoint: anything that can reach the
	// panel can read it.
	for _, secret := range []string{"secret-jira-token", "another-secret", "gh-secret"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("connectors response leaked a credential")
		}
	}

	var got Connectors
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]ConnectorAdapter{}
	for _, adapter := range got.Adapters {
		byID[adapter.ID] = adapter
	}

	jira, ok := byID["atlassian"]
	if !ok {
		t.Fatalf("adapters = %+v, want atlassian", got.Adapters)
	}
	if jira.Label != "Jira" || !jira.Installed {
		t.Errorf("atlassian = %+v, want label Jira and installed", jira)
	}
	if len(jira.Organizations) != 2 {
		t.Fatalf("atlassian organisations = %+v, want two", jira.Organizations)
	}
	// The default organisation sorts first, because it is the one delivery
	// work uses when it names none.
	if !jira.Organizations[0].Default || jira.Organizations[0].ID != "gdncomm" {
		t.Errorf("organisations = %+v, want gdncomm first and marked default", jira.Organizations)
	}
	if jira.Organizations[0].AdapterID != "atlassian:gdncomm" {
		t.Errorf("adapter_id = %q, want atlassian:gdncomm", jira.Organizations[0].AdapterID)
	}
	if jira.Organizations[0].LastVerifiedAt == nil {
		t.Error("a verified organisation should report when")
	}

	if docling, ok := byID["docling"]; !ok || docling.Installed {
		t.Errorf("docling = %+v, want present and installed:false", docling)
	}

	// GitHub has credentials but no adapter configured - exactly the case
	// worth surfacing, since those credentials cannot be used yet.
	gh, ok := byID["github"]
	if !ok {
		t.Fatalf("adapters = %+v, want github listed from its credentials alone", got.Adapters)
	}
	if gh.Installed || len(gh.Organizations) != 1 {
		t.Errorf("github = %+v, want not installed with one organisation", gh)
	}
}

func TestConnectorsWithNothingConfigured(t *testing.T) {
	store := providercreds.Open(filepath.Join(t.TempDir(), "credentials.yaml"))
	rec := httptest.NewRecorder()
	ConnectorsHandler(func() map[string]adapters.AdapterSpec { return nil }, store)(rec, httptest.NewRequest(http.MethodGet, "/api/v1/connectors", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got Connectors
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Adapters) != 0 {
		t.Errorf("adapters = %+v, want none", got.Adapters)
	}
	if got.CredentialsPath == "" {
		t.Error("the response should still say where credentials would live")
	}
}
