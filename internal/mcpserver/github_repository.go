package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/providercreds"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// repositorySearchLimit caps how many matches one search offers. Beyond a
// handful, a name is not ambiguous - it is unspecific, and the answer is
// a better name rather than a longer list.
const repositorySearchLimit = 10

// resolveGitHubRepository turns whatever a caller named - nothing, a bare
// repository name, or a full "owner/repo" - into the one repository to
// act on, or the question whose answer would settle it.
//
// The tools used to take the caller's string verbatim, which works only
// while the caller already knows both the owner and which organisation's
// credential reaches it. Naming a repository that this host cannot route
// produced an error about an unconfigured organisation, and naming none
// produced an error about a missing argument, when in both cases the
// answer was sitting either in the checkout's own remote or one search
// away.
//
// Nothing here guesses across organisations: a repository is searched for
// under the credential that would be used to act on it, and every other
// organisation is offered as a choice rather than tried, because which
// credential writes a review is exactly the decision a human owns.
func resolveGitHubRepository(ctx context.Context, a *app.App, store *delivery.Store, named string) (string, *protocol.NeedUserInput, error) {
	named = strings.Trim(strings.TrimSpace(named), "/")
	if owner, _, qualified := strings.Cut(named, "/"); qualified && owner != "" {
		return named, nil, nil
	}
	if named == "" {
		if slug, ok := originRepositorySlug(ctx, a); ok {
			return slug, nil, nil
		}
		return "", nil, fmt.Errorf("mcpserver: no repository named, and this workspace has no GitHub remote to take one from")
	}

	orgs, err := candidateGitHubOrgs(a)
	if err != nil {
		return "", nil, err
	}
	if len(orgs) == 0 {
		return "", nil, fmt.Errorf("mcpserver: %q names no owner, and no GitHub organisation is configured to search; run punakawan setup github", named)
	}

	matches, err := searchGitHubRepositories(ctx, a, orgs[0], named)
	if err != nil {
		return "", nil, err
	}
	switch len(matches) {
	case 1:
		if store != nil {
			// Remembering it here is what keeps this a question asked
			// once: the next call for the same repository routes straight
			// through the project that now records which credential
			// reached it.
			if err := store.RememberGitHubOrg(ctx, matches[0], orgs[0].ID); err != nil {
				return "", nil, err
			}
		}
		return matches[0], nil, nil
	case 0:
		if len(orgs) == 1 {
			return "", nil, fmt.Errorf("mcpserver: %s has no repository named %q", gitHubOrgLabel(orgs[0]), named)
		}
		return "", noRepositoryAtDefaultOrg(orgs, named), nil
	default:
		return "", repositoryChoice(orgs[0], named, matches), nil
	}
}

// originRepositorySlug reads "owner/repo" out of this workspace's own
// origin remote - the cheapest correct answer there is, and one that
// needs no network and no credential.
//
// The workspace root is tried first and its configured repositories
// after, because a single-repository workspace is usually rooted at the
// checkout while a multi-repository one is rooted above them. A workspace
// whose repositories disagree about their origin answers nothing: that is
// a choice, not a default.
func originRepositorySlug(ctx context.Context, a *app.App) (string, bool) {
	if a.Inspector == nil || a.Workspace == nil {
		return "", false
	}
	if slug, ok := originSlugAt(ctx, a, a.Workspace.Root); ok {
		return slug, true
	}
	found := ""
	for _, repo := range a.Workspace.Repositories {
		path, err := a.Workspace.RepositoryPath(repo.ID)
		if err != nil {
			continue
		}
		slug, ok := originSlugAt(ctx, a, path)
		if !ok || slug == found {
			continue
		}
		if found != "" {
			return "", false
		}
		found = slug
	}
	return found, found != ""
}

func originSlugAt(ctx context.Context, a *app.App, path string) (string, bool) {
	remotes, err := a.Inspector.Remotes(ctx, path)
	if err != nil {
		return "", false
	}
	for _, remote := range remotes {
		if remote.Name != "origin" {
			continue
		}
		if slug, ok := gitops.RepoSlug(remote.FetchUrl); ok {
			return slug, true
		}
	}
	return "", false
}

func candidateGitHubOrgs(a *app.App) ([]providercreds.Org, error) {
	if a.Credentials == nil {
		return nil, nil
	}
	orgs, err := a.Credentials.Candidates(providercreds.ProviderGitHub)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: read configured GitHub organisations: %w", err)
	}
	return orgs, nil
}

// searchGitHubRepositories asks one organisation's adapter for the
// repositories matching name, under both the organisation's own id and
// the account its credential belongs to - those are routinely different
// names for the same place.
func searchGitHubRepositories(ctx context.Context, a *app.App, org providercreds.Org, name string) ([]string, error) {
	gate, err := a.AdapterRegistry.Gate(ctx, adapters.QualifyAdapterID("github", org.ID))
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open github adapter for %s: %w", org.ID, err)
	}
	owners := []string{org.ID}
	if account := strings.TrimSpace(org.Account); account != "" && !strings.EqualFold(account, org.ID) {
		owners = append(owners, account)
	}
	raw, err := gate.Call(ctx, "repository-search:"+name, "github.searchRepositories", map[string]any{
		"name": name, "owners": owners, "limit": repositorySearchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("mcpserver: search %s for a repository named %q: %w", gitHubOrgLabel(org), name, err)
	}
	var decoded struct {
		Normalized struct {
			Repositories []struct {
				Repository string `json:"repository"`
				Private    *bool  `json:"private"`
			} `json:"repositories"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("mcpserver: decode github.searchRepositories response: %w", err)
	}
	out := make([]string, 0, len(decoded.Normalized.Repositories))
	for _, repo := range decoded.Normalized.Repositories {
		if repo.Repository != "" {
			out = append(out, repo.Repository)
		}
	}
	sort.Strings(out)
	return out, nil
}

func repositoryChoice(org providercreds.Org, name string, matches []string) *protocol.NeedUserInput {
	options := make([]protocol.NeedUserInputOptionsElem, 0, len(matches))
	for _, repository := range matches {
		options = append(options, protocol.NeedUserInputOptionsElem{
			Id: repository, Label: repository,
			Impact: "Reads and writes pull requests on " + repository,
		})
	}
	return &protocol.NeedUserInput{
		Kind:          protocol.NeedUserInputKindDecisionRequired,
		Question:      fmt.Sprintf("%q matches %d repositories %s can see. Which one?", name, len(matches), gitHubOrgLabel(org)),
		MissingFields: []string{"repository"},
		Options:       options,
	}
}

func noRepositoryAtDefaultOrg(orgs []providercreds.Org, name string) *protocol.NeedUserInput {
	options := make([]protocol.NeedUserInputOptionsElem, 0, len(orgs)-1)
	for _, org := range orgs[1:] {
		owner := strings.TrimSpace(org.Account)
		if owner == "" {
			owner = org.ID
		}
		options = append(options, protocol.NeedUserInputOptionsElem{
			Id: owner + "/" + name, Label: gitHubOrgLabel(org),
			Impact: "Uses " + org.ID + "'s credential for this repository",
		})
	}
	return &protocol.NeedUserInput{
		Kind:          protocol.NeedUserInputKindDecisionRequired,
		Question:      fmt.Sprintf("%s has no repository named %q. Which organisation holds it?", gitHubOrgLabel(orgs[0]), name),
		MissingFields: []string{"repository"},
		Options:       options,
	}
}

func gitHubOrgLabel(org providercreds.Org) string {
	if account := strings.TrimSpace(org.Account); account != "" && !strings.EqualFold(account, org.ID) {
		return fmt.Sprintf("organisation %s (%s)", org.ID, account)
	}
	return "organisation " + org.ID
}
