package mcpserver

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// resolveGithubRepo resolves repoPath's origin remote to a GitHub
// "owner/repo" slug and opens the GitHub adapter's Gate, for tools that
// only need to read from the provider (review_pr, fix_pr_review's comment
// fetch) - unlike create_pr's unavailableReason, this does not check push
// capability, since a read-only reviewer role may legitimately have no
// push access at all.
func resolveGithubRepo(ctx context.Context, inspector *gitops.Inspector, registry adapterGateProvider, repoPath string) (slug string, gate *adapters.Gate, err error) {
	caps, err := inspector.DetectCapabilities(ctx, repoPath, "origin")
	if err != nil {
		return "", nil, fmt.Errorf("mcpserver: detect git capabilities: %w", err)
	}
	if caps.Provider == nil || *caps.Provider != protocol.GitCapabilitiesProviderGithub {
		return "", nil, fmt.Errorf("mcpserver: no github remote detected for this repository")
	}

	var origin *protocol.GitCapabilitiesRemotesElem
	for i := range caps.Remotes {
		if caps.Remotes[i].Name == "origin" {
			origin = &caps.Remotes[i]
			break
		}
	}
	slug, ok := gitops.RepoSlug(origin.FetchUrl)
	if !ok {
		return "", nil, fmt.Errorf("mcpserver: could not determine owner/repo from remote %q", origin.FetchUrl)
	}

	gate, err = registry.Gate(ctx, "github")
	if err != nil {
		return "", nil, fmt.Errorf("mcpserver: no github adapter configured: %w", err)
	}
	return slug, gate, nil
}
