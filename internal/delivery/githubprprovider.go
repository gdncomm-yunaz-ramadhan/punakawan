// githubprprovider.go implements PRProvider for GitHub, the same way
// githubidentity.go implements this domain's GitHub-specific preflight
// checks: by calling the GitHub adapter's github.createPullRequest
// operation through an adapterGate, never through internal/mcpserver.
package delivery

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// GitHubPRProvider publishes pull requests on GitHub via Gate.
type GitHubPRProvider struct {
	Gate adapterGate
}

// Publish calls github.createPullRequest and returns the normalized
// number/url this codebase's github adapter reports for a created pull
// request (packages/github-adapter/src/normalize.ts's normalizePullRequest:
// "number" and "url").
func (p GitHubPRProvider) Publish(ctx context.Context, req PublishPRRequest) (int, string, error) {
	raw, err := p.Gate.Call(ctx, req.RepoSlug, "github.createPullRequest", map[string]any{
		"repository": req.RepoSlug,
		"baseBranch": req.BaseBranch,
		"headBranch": req.HeadBranch,
		"title":      req.Title,
		"body":       req.Body,
	})
	if err != nil {
		return 0, "", fmt.Errorf("delivery: github.createPullRequest for %s: %w", req.RepoSlug, err)
	}

	var result struct {
		Normalized struct {
			Number int    `json:"number"`
			Url    string `json:"url"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, "", fmt.Errorf("delivery: decode github.createPullRequest response for %s: %w", req.RepoSlug, err)
	}
	return result.Normalized.Number, result.Normalized.Url, nil
}

// ProviderName reports GitHubPRProvider's pr_provider enum value, per
// PublishPullRequest's optional namedPRProvider capability.
func (p GitHubPRProvider) ProviderName() protocol.DeliveryLanePrProvider {
	return protocol.DeliveryLanePrProviderGithub
}
