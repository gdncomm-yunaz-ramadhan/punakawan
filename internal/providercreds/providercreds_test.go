package providercreds

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeriveOrgIDFromTheURLAPersonAlreadyKnows(t *testing.T) {
	cases := []struct {
		provider Provider
		raw      string
		want     string
	}{
		{ProviderJira, "https://gdncomm.atlassian.net", "gdncomm"},
		{ProviderJira, "gdncomm.atlassian.net", "gdncomm"},
		{ProviderJira, "https://GDNComm.atlassian.net/jira/software", "gdncomm"},
		{ProviderJira, "https://jira.acme.com", "acme"},
		{ProviderJira, "https://issues.acme.co.uk/jira", "acme"},
		{ProviderGitHub, "https://github.com/acme", "acme"},
		{ProviderGitHub, "https://github.com/acme/some-repo", "acme"},
		{ProviderGitHub, "https://github.acme.com", "acme"},
	}
	for _, tc := range cases {
		got, err := DeriveOrgID(tc.provider, tc.raw)
		if err != nil {
			t.Errorf("DeriveOrgID(%s, %q): %v", tc.provider, tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("DeriveOrgID(%s, %q) = %q, want %q", tc.provider, tc.raw, got, tc.want)
		}
	}

	// github.com with no organisation in the path names nobody, and
	// guessing "github" would create an organisation that is not one.
	if _, err := DeriveOrgID(ProviderGitHub, "https://github.com"); err == nil {
		t.Error("DeriveOrgID accepted https://github.com, want it to ask for the organisation's own URL")
	}
	if _, err := DeriveOrgID(ProviderJira, "ftp://acme.com"); err == nil {
		t.Error("DeriveOrgID accepted a non-http scheme")
	}
}

func TestStoreKeepsOneEntryPerProviderAndOrg(t *testing.T) {
	store := Open(filepath.Join(t.TempDir(), "nested", "credentials.yaml"))

	if _, err := store.Get(ProviderJira, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on an empty store = %v, want ErrNotFound", err)
	}

	first := Org{ID: "GDNComm", Provider: ProviderJira, BaseURL: "https://gdncomm.atlassian.net", Email: "a@example.com", Token: "t1"}
	if err := store.Put(first); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// The same organisation for a different provider is a different entry.
	if err := store.Put(Org{ID: "gdncomm", Provider: ProviderGitHub, BaseURL: "https://github.com", Token: "gh"}); err != nil {
		t.Fatalf("Put github: %v", err)
	}

	got, err := store.Get(ProviderJira, "gdncomm")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Token != "t1" || got.Host() != "gdncomm.atlassian.net" {
		t.Fatalf("Get = %+v, want the stored Jira credentials", got)
	}
	if !got.Default {
		t.Fatal("the first organisation for a provider must become its default, so a machine with one never has to name it")
	}
	// Ids are matched case-insensitively, or the same organisation typed
	// two ways becomes two organisations - which is the bug this replaces.
	if _, err := store.Get(ProviderJira, "GDNCOMM"); err != nil {
		t.Fatalf("Get with different casing: %v", err)
	}

	updated := first
	updated.Token = "t2"
	if err := store.Put(updated); err != nil {
		t.Fatalf("Put again: %v", err)
	}
	orgs, err := store.ListFor(ProviderJira)
	if err != nil || len(orgs) != 1 {
		t.Fatalf("ListFor = %+v, %v; want the re-put organisation to replace, not duplicate", orgs, err)
	}
	if orgs[0].Token != "t2" || !orgs[0].Default {
		t.Fatalf("re-put organisation = %+v, want the new token and the default kept", orgs[0])
	}

	if err := store.Put(Org{ID: "acme", Provider: ProviderJira, BaseURL: "https://acme.atlassian.net", Token: "t3"}); err != nil {
		t.Fatalf("Put second Jira org: %v", err)
	}
	// A second organisation does not disturb the default, so the machine
	// keeps working without anyone naming one.
	if def, err := store.Get(ProviderJira, ""); err != nil || def.ID != "gdncomm" {
		t.Fatalf("Get with no id = %+v, %v; want the default kept", def, err)
	}

	if err := store.MarkVerified(ProviderJira, "gdncomm", time.Now()); err != nil {
		t.Fatalf("MarkVerified: %v", err)
	}
	if err := store.Remove(ProviderJira, "gdncomm"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	promoted, err := store.Get(ProviderJira, "")
	if err != nil {
		t.Fatalf("Get after removing the default: %v", err)
	}
	if promoted.ID != "acme" {
		t.Fatalf("after removing the default, Get = %q; want the remaining organisation promoted", promoted.ID)
	}
}

func TestStoreFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.yaml")
	store := Open(path)
	if err := store.Put(Org{ID: "acme", Provider: ProviderJira, BaseURL: "https://acme.atlassian.net", Token: "secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("credentials file mode = %o, want 0600 - it holds provider tokens in plain text", perm)
	}
}

func TestAdapterIDRoundTripsTheOrg(t *testing.T) {
	id := ProviderJira.AdapterID("gdncomm")
	if id != "atlassian:gdncomm" {
		t.Fatalf("AdapterID = %q, want atlassian:gdncomm", id)
	}
	base, org := SplitAdapterID(id)
	if base != "atlassian" || org != "gdncomm" {
		t.Fatalf("SplitAdapterID(%q) = %q, %q", id, base, org)
	}
	// An id from before organisations existed still names its adapter.
	if base, org := SplitAdapterID("atlassian"); base != "atlassian" || org != "" {
		t.Fatalf("SplitAdapterID(atlassian) = %q, %q; want the bare adapter and no org", base, org)
	}
}

// TestGetRefusesToGuessWhenAFileNamesNoDefault covers a hand-edited
// credentials file: with several organisations and no default marked,
// picking one would silently send work to the wrong organisation.
func TestGetRefusesToGuessWhenAFileNamesNoDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.yaml")
	body := "version: " + SupportedVersion + "\norgs:\n" +
		"  - id: acme\n    provider: jira\n    base_url: https://acme.atlassian.net\n    token: a\n" +
		"  - id: other\n    provider: jira\n    base_url: https://other.atlassian.net\n    token: b\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}

	_, err := Open(path).Get(ProviderJira, "")
	if err == nil {
		t.Fatal("Get picked an organisation with no default set; it must ask which")
	}
	if !strings.Contains(err.Error(), "acme") || !strings.Contains(err.Error(), "other") {
		t.Fatalf("error = %v, want it to name the organisations to choose from", err)
	}
}
