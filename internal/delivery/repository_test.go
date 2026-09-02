package delivery

import (
	"context"
	"database/sql"
	"testing"
)

func TestRepositoryIdentityNormalizesCommonGitRemotes(t *testing.T) {
	for _, repositoryURL := range []string{
		"git@github.com:acme/payments.git",
		"https://github.com/acme/payments.git",
		"ssh://git@github.com/acme/payments.git",
		"git://github.com/acme/payments",
	} {
		if got, want := RepositoryIdentity(repositoryURL), "github.com/acme/payments"; got != want {
			t.Errorf("RepositoryIdentity(%q) = %q, want %q", repositoryURL, got, want)
		}
	}
}

func TestRepositoryURLVariantsIncludeEquivalentSSHAndHTTPSForms(t *testing.T) {
	variants := repositoryURLVariants("git@github.com:acme/payments.git")
	want := map[string]bool{
		"git@github.com:acme/payments.git":     false,
		"https://github.com/acme/payments.git": false,
	}
	for _, variant := range variants {
		if _, ok := want[variant]; ok {
			want[variant] = true
		}
	}
	for variant, found := range want {
		if !found {
			t.Errorf("variants missing %q: %v", variant, variants)
		}
	}
}

func TestFindProjectsByRepositoryURLFindsLegacyEquivalentRawURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	err := s.db.Write(ctx, "legacy-project", "insert legacy project", func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO delivery_projects (id, slug, repository_url, repository_identity, default_branch, status, registered_at, revision)
			VALUES ('legacy', 'payments', 'https://github.com/acme/payments.git', '', 'main', 'active', ?, 0)`,
			"2026-01-01T00:00:00Z")
		return err
	})
	if err != nil {
		t.Fatalf("insert legacy project: %v", err)
	}

	projects, err := s.FindProjectsByRepositoryURL(ctx, "git@github.com:acme/payments.git")
	if err != nil {
		t.Fatalf("FindProjectsByRepositoryURL: %v", err)
	}
	if len(projects) != 1 || projects[0].Slug != "payments" {
		t.Fatalf("projects = %+v, want legacy payments project", projects)
	}
}
