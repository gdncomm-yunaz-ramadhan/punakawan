package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/providercreds"
	"github.com/ygrip/punakawan/internal/workspace"
)

// isolatedCredentials points the storage kernel at a temp directory so a
// test can never read - or overwrite - the credentials of the person
// running it.
func isolatedCredentials(t *testing.T) *providercreds.Store {
	t.Helper()
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	path, err := workspace.GlobalCredentialsPath()
	if err != nil {
		t.Fatalf("resolve credentials path: %v", err)
	}
	return providercreds.Open(path)
}

func runSetupProvider(t *testing.T, provider providercreds.Provider, args ...string) (string, error) {
	t.Helper()
	cmd := newSetupProviderCmd(provider)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// A bytes.Reader is not a terminal, so every prompt falls through to
	// its flag value instead of blocking on a read that never returns.
	cmd.SetIn(bytes.NewReader(nil))
	cmd.SetArgs(args)
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	return out.String(), err
}

func TestSetupProviderSavesNothingWhenVerificationFails(t *testing.T) {
	store := isolatedCredentials(t)

	// .invalid never resolves, so the probe fails the way a wrong host or
	// a dead token does, without reaching anyone's real Jira site.
	out, err := runSetupProvider(t, providercreds.ProviderJira,
		"--url", "https://acme-test.invalid", "--token", "not-a-real-token", "--email", "someone@example.test")
	if err == nil {
		t.Fatal("expected an unreachable site to fail setup")
	}
	if !strings.Contains(err.Error(), "Nothing was saved") {
		t.Errorf("error should say the machine was left untouched, got: %v", err)
	}
	if !strings.Contains(err.Error(), "id.atlassian.com") {
		t.Errorf("error should name where to create a token, got: %v", err)
	}
	if !strings.Contains(out, "Organisation: acme-test") {
		t.Errorf("output should report the derived organisation, got: %q", out)
	}

	if _, statErr := os.Stat(store.Path()); !os.IsNotExist(statErr) {
		t.Fatalf("a failed verification must leave no credential file, stat err = %v", statErr)
	}
}

func TestSetupProviderListsAndRemovesOrganisations(t *testing.T) {
	store := isolatedCredentials(t)
	for _, org := range []providercreds.Org{
		{ID: "gdncomm", Provider: providercreds.ProviderJira, BaseURL: "https://gdncomm.atlassian.net", Email: "a@example.test", Token: "t1", LastVerifiedAt: time.Now().UTC()},
		{ID: "acme", Provider: providercreds.ProviderJira, BaseURL: "https://acme.atlassian.net", Email: "b@example.test", Token: "t2"},
	} {
		if err := store.Put(org); err != nil {
			t.Fatalf("seed %s: %v", org.ID, err)
		}
	}

	out, err := runSetupProvider(t, providercreds.ProviderJira, "--list")
	if err != nil {
		t.Fatalf("--list: %v", err)
	}
	for _, want := range []string{"gdncomm", "acme", "https://acme.atlassian.net", "never"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q, got:\n%s", want, out)
		}
	}

	if _, err := runSetupProvider(t, providercreds.ProviderJira, "--remove", "acme"); err != nil {
		t.Fatalf("--remove: %v", err)
	}
	orgs, err := store.ListFor(providercreds.ProviderJira)
	if err != nil {
		t.Fatalf("list after remove: %v", err)
	}
	if len(orgs) != 1 || orgs[0].ID != "gdncomm" {
		t.Fatalf("remove should leave only gdncomm, got %+v", orgs)
	}
}

func TestSetupProviderEmptyListingNamesTheCommandToRun(t *testing.T) {
	isolatedCredentials(t)
	out, err := runSetupProvider(t, providercreds.ProviderGitHub, "--list")
	if err != nil {
		t.Fatalf("--list: %v", err)
	}
	if !strings.Contains(out, "punakawan setup github") {
		t.Errorf("an empty listing should name the command that fixes it, got: %q", out)
	}
}

func TestGitHubAPIBaseURLDistinguishesEnterprise(t *testing.T) {
	for _, tc := range []struct{ baseURL, want string }{
		{"https://github.com/acme", "https://api.github.com"},
		{"https://git.acme.example/acme", "https://git.acme.example/api/v3"},
	} {
		got := gitHubAPIBaseURL(providercreds.Org{Provider: providercreds.ProviderGitHub, BaseURL: tc.baseURL})
		if got != tc.want {
			t.Errorf("gitHubAPIBaseURL(%q) = %q, want %q", tc.baseURL, got, tc.want)
		}
	}
}

func TestSetupCommandExposesBothProviders(t *testing.T) {
	var names []string
	for _, sub := range newSetupCmd().Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"jira", "github"} {
		if !containsString(names, want) {
			t.Errorf("`punakawan setup` should expose the %q subcommand, got %v", want, names)
		}
	}
}

func containsString(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}
