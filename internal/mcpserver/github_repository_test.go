package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/providercreds"
)

// A repository the caller did not name is sitting in the checkout's own
// remote; asking for it, or failing on a missing argument, is work the
// caller should never have had to do.
func TestResolveGitHubRepositoryTakesTheOriginWhenNoneIsNamed(t *testing.T) {
	a := newTestApp(t)
	repoDir, err := a.Workspace.RepositoryPath("repo-a")
	if err != nil {
		t.Fatalf("RepositoryPath: %v", err)
	}
	runGit(t, repoDir, "remote", "add", "origin", "git@github.com:personal-account/widgets.git")

	repository, needsInput, err := resolveGitHubRepository(context.Background(), a, nil, "")
	if err != nil || needsInput != nil {
		t.Fatalf("resolveGitHubRepository = %q needsInput:%v err:%v", repository, needsInput, err)
	}
	if repository != "personal-account/widgets" {
		t.Fatalf("repository = %q, want the origin's slug", repository)
	}

	// An already-qualified repository is the caller's own statement and
	// is taken as given - no search, no question.
	repository, needsInput, err = resolveGitHubRepository(context.Background(), a, nil, "acme/other")
	if err != nil || needsInput != nil || repository != "acme/other" {
		t.Fatalf("resolveGitHubRepository(acme/other) = %q needsInput:%v err:%v", repository, needsInput, err)
	}
}

// Several matches is the case the whole ladder exists for: one option per
// repository, each id exactly what the caller passes back.
func TestRepositoryChoiceOffersEveryMatchByItsFullName(t *testing.T) {
	org := providercreds.Org{ID: "acme", Account: "acme-bot"}

	need := repositoryChoice(org, "widgets", []string{"acme/widgets", "acme-bot/widgets"})
	if len(need.Options) != 2 || need.Options[0].Id != "acme/widgets" || need.Options[1].Id != "acme-bot/widgets" {
		t.Fatalf("options = %+v, want one per match keyed by full name", need.Options)
	}
	if !strings.Contains(need.Question, "acme-bot") {
		t.Fatalf("question = %q, want it to name the organisation searched", need.Question)
	}

	// Nothing at the default organisation offers the others rather than
	// silently reaching for another credential.
	others := noRepositoryAtDefaultOrg([]providercreds.Org{org, {ID: "personal", Account: "someone"}}, "widgets")
	if len(others.Options) != 1 || others.Options[0].Id != "someone/widgets" {
		t.Fatalf("options = %+v, want the other organisation's own owner", others.Options)
	}
}
