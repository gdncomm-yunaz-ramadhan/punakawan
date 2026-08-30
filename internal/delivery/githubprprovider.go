// githubprprovider.go implements PRProvider for GitHub. github.createPullRequest
// is a side-effecting adapter operation, so it can no longer be called
// directly through an adapterGate the way githubidentity.go's read-only
// preflight checks are - it has to be enqueued on the durable provider
// outbox and resolved from there, the same as every other provider write in
// this codebase. That is a deliberate, narrow exception to this package's
// usual practice of never importing internal/adapters directly: this is the
// one write GitHubPRProvider performs, and nothing else in this package
// needs adapter visibility as a result.
package delivery

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/providerwrite"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// GitHubPRProvider publishes pull requests on GitHub by enqueuing and
// synchronously resolving a github.createPullRequest outbox intent.
type GitHubPRProvider struct {
	Outbox   *outbox.Store
	Adapters *adapters.Registry
}

// Publish enqueues github.createPullRequest for req and waits for it to
// resolve, returning the normalized number/url this codebase's github
// adapter reports for a created pull request
// (packages/github-adapter/src/normalize.ts's normalizePullRequest: "number"
// and "url"). The intent's OperationFingerprint makes a retried publish for
// the same repository/head/base resolve to the same durable row instead of
// opening a second pull request.
func (p GitHubPRProvider) Publish(ctx context.Context, req PublishPRRequest) (int, string, error) {
	payload, err := json.Marshal(map[string]any{
		"repository": req.RepoSlug,
		"baseBranch": req.BaseBranch,
		"headBranch": req.HeadBranch,
		"title":      req.Title,
		"body":       req.Body,
	})
	if err != nil {
		return 0, "", fmt.Errorf("delivery: encode github.createPullRequest payload for %s: %w", req.RepoSlug, err)
	}

	resolved, err := providerwrite.ExecuteNow(ctx, p.Outbox, p.Adapters, "delivery-github-pr-publish", outbox.Intent{
		AdapterID:            "github",
		Operation:            "github.createPullRequest",
		TargetKey:            req.RepoSlug,
		PayloadJSON:          string(payload),
		OperationFingerprint: providerwrite.GitHubCreatePRFingerprint(req.RepoSlug, req.HeadBranch, req.BaseBranch),
	})
	if err != nil {
		return 0, "", fmt.Errorf("delivery: enqueue github.createPullRequest for %s: %w", req.RepoSlug, err)
	}
	if resolved.Status != outbox.StatusSucceeded {
		reason := resolved.LastErrorRedacted
		if reason == "" {
			reason = fmt.Sprintf("intent ended in status %q", resolved.Status)
		}
		return 0, "", fmt.Errorf("delivery: github.createPullRequest for %s did not succeed: %s", req.RepoSlug, reason)
	}

	number := 0
	fmt.Sscanf(resolved.ExternalID, "%d", &number)
	url := ""
	effects, err := p.Outbox.ListEffects(ctx, resolved.ID)
	if err != nil {
		return 0, "", fmt.Errorf("delivery: read github.createPullRequest effects for %s: %w", req.RepoSlug, err)
	}
	for _, effect := range effects {
		if effect.EffectKey == "url" {
			url = effect.ExternalID
		}
	}
	return number, url, nil
}

// ProviderName reports GitHubPRProvider's pr_provider enum value, per
// PublishPullRequest's optional namedPRProvider capability.
func (p GitHubPRProvider) ProviderName() protocol.DeliveryLanePrProvider {
	return protocol.DeliveryLanePrProviderGithub
}
