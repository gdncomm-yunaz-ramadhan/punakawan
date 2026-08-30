// githubprprovider.go implements PRProvider for GitHub by delegating to
// internal/githubintegration.Service, which enqueues and resolves the
// underlying github.createPullRequest outbox intent - the same durable
// outbox path every other provider write in this codebase goes through.
// That is a deliberate, narrow exception to this package's usual practice
// of never importing internal/adapters directly: this is the one write
// GitHubPRProvider performs, and nothing else in this package needs
// adapter visibility as a result.
package delivery

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/githubintegration"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// GitHubPRProvider publishes pull requests on GitHub via
// githubintegration.Service.
type GitHubPRProvider struct {
	Outbox   *outbox.Store
	Adapters *adapters.Registry
}

// Publish opens req as a pull request and waits for it to resolve,
// returning the normalized number/url this codebase's github adapter
// reports for a created pull request
// (packages/github-adapter/src/normalize.ts's normalizePullRequest: "number"
// and "url"). A retried publish for the same repository/head/base resolves
// to the same durable intent instead of opening a second pull request.
func (p GitHubPRProvider) Publish(ctx context.Context, req PublishPRRequest) (int, string, error) {
	svc := githubintegration.NewService(p.Adapters, p.Outbox)
	number, url, err := svc.CreatePullRequest(ctx, githubintegration.CreatePullRequestRequest{
		RunID:      "delivery-github-pr-publish",
		Repository: req.RepoSlug,
		BaseBranch: req.BaseBranch,
		HeadBranch: req.HeadBranch,
		Title:      req.Title,
		Body:       req.Body,
	})
	if err != nil {
		return 0, "", fmt.Errorf("delivery: publish pull request for %s: %w", req.RepoSlug, err)
	}
	return number, url, nil
}

// ProviderName reports GitHubPRProvider's pr_provider enum value, per
// PublishPullRequest's optional namedPRProvider capability.
func (p GitHubPRProvider) ProviderName() protocol.DeliveryLanePrProvider {
	return protocol.DeliveryLanePrProviderGithub
}
