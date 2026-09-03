package delivery

import (
	"context"
	"testing"
)

// The organisation a repository is reached through is remembered where
// the repository already is, because an owner is not an organisation id:
// a credential holds an account of whatever name its token belongs to.
func TestRememberGitHubOrgIsReadBackForTheSameRepository(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.UpsertProject(ctx, "upsert-1", NewID(), "widgets", "https://github.com/personal-account/widgets.git", "main"); err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	if _, ok, err := s.GitHubOrgForRepository(ctx, "personal-account/widgets"); err != nil || ok {
		t.Fatalf("GitHubOrgForRepository before anything was remembered = ok:%v err:%v, want no answer", ok, err)
	}

	if err := s.RememberGitHubOrg(ctx, "personal-account/widgets", "acme"); err != nil {
		t.Fatalf("RememberGitHubOrg: %v", err)
	}
	// The registered url carries a .git suffix the caller's slug does not:
	// repository identity has to match them, or the answer is recorded
	// somewhere nothing reads it back.
	org, ok, err := s.GitHubOrgForRepository(ctx, "personal-account/widgets")
	if err != nil || !ok || org != "acme" {
		t.Fatalf("GitHubOrgForRepository = %q ok:%v err:%v, want acme", org, ok, err)
	}

	// A repository no project is registered for remembers nothing rather
	// than inventing a project to hang it on.
	if err := s.RememberGitHubOrg(ctx, "someone/unregistered", "acme"); err != nil {
		t.Fatalf("RememberGitHubOrg for an unregistered repository: %v", err)
	}
	if _, ok, err := s.GitHubOrgForRepository(ctx, "someone/unregistered"); err != nil || ok {
		t.Fatalf("GitHubOrgForRepository for an unregistered repository = ok:%v err:%v, want no answer", ok, err)
	}
}
