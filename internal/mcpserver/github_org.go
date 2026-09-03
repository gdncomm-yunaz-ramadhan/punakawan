package mcpserver

import (
	"context"
	"strings"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/githubintegration"
	"github.com/ygrip/punakawan/internal/providercreds"
)

// gitHubOrgResolver resolves which configured organisation's credential
// reaches a repository, so the adapter that speaks for it is the one that
// can actually see it.
//
// The repository owner used to stand in for the organisation directly.
// That holds only while every organisation is named after its owner: a
// credential configured from one site holds an account of whatever name
// its token belongs to, so a personal or sub-organisation repository
// resolved to an adapter id no credential answered for, and the call
// failed with a "run punakawan setup" message for an organisation that
// was already set up. The project's remembered answer wins when there is
// one, because it is the one that was proven to work.
func gitHubOrgResolver(a *app.App, store *delivery.Store) githubintegration.Option {
	return githubintegration.WithRepositoryOrgResolver(func(ctx context.Context, repository string) (string, bool) {
		if store != nil {
			if org, ok, err := store.GitHubOrgForRepository(ctx, repository); err == nil && ok {
				return org, true
			}
		}
		if a.Credentials == nil {
			return "", false
		}
		owner, _, _ := strings.Cut(strings.TrimSpace(repository), "/")
		org, ok, err := a.Credentials.MatchOwner(providercreds.ProviderGitHub, owner)
		if err != nil || !ok {
			return "", false
		}
		return org.ID, true
	})
}
