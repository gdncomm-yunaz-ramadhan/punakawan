// Package githubintegration implements the GitHub side of pull request
// hydration and review submission as a small set of named operations,
// shared by every caller that needs a complete, stale-safe view of a pull
// request or needs to open one/review one without duplicating that logic.
//
// Every write Service performs is enqueued through the durable outbox
// (internal/outbox/internal/providerwrite) and resolved synchronously via
// providerwrite.ExecuteNow, exactly like every other provider write in this
// codebase - Service never calls an adapter write directly, and never
// replays a write it cannot confirm; reconciling an ambiguous attempt is
// internal/providerwrite's own registered reconcilers' job (see
// ReconcileGitHubCreatePR/ReconcileGitHubReview and friends), not
// something this package duplicates.
package githubintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/providercreds"
	"github.com/ygrip/punakawan/internal/providerwrite"
)

// GateResolver is the subset of *adapters.Registry's behavior Service
// depends on, so a test can substitute a fake instead of spawning a real
// adapter subprocess.
type GateResolver interface {
	Gate(ctx context.Context, adapterID string) (*adapters.Gate, error)
}

// RepositoryOrgResolver names the configured organisation whose
// credentials reach a repository, reporting false when this host has
// nothing configured for it. It takes the whole repository rather than
// its owner because the owner in "owner/repo" is not necessarily an
// organisation id: a credential configured from one site routinely holds
// an account of a different name, and a repository already resolved once
// is remembered against the repository itself.
type RepositoryOrgResolver func(ctx context.Context, repository string) (orgID string, ok bool)

// Option configures a Service.
type Option func(*Service)

// WithRepositoryOrgResolver wires organisation resolution. Without it a
// Service keeps deriving the organisation from the owner alone, which is
// correct for a host whose organisations are named after their owners and
// is what every caller did before.
func WithRepositoryOrgResolver(resolve RepositoryOrgResolver) Option {
	return func(s *Service) { s.orgs = resolve }
}

// Service implements GitHub pull request hydration and review submission.
type Service struct {
	registry GateResolver
	outbox   *outbox.Store
	orgs     RepositoryOrgResolver
}

// NewService builds a Service.
func NewService(registry GateResolver, outboxStore *outbox.Store, opts ...Option) *Service {
	s := &Service{registry: registry, outbox: outboxStore}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// ErrReviewProposalStale is returned by SubmitReview when the pull
// request's current head no longer matches the SHA the review was
// proposed against. This is a normal conflict requiring the caller to
// re-hydrate the pull request and re-propose its review against the new
// head - not a confirmation prompt, and not retried automatically: the
// findings a review carries are about a specific diff, and that diff no
// longer exists once the head has moved.
type ErrReviewProposalStale struct {
	Repository        string
	PullRequestNumber int
	ProposedHeadSHA   string
	CurrentHeadSHA    string
}

func (e *ErrReviewProposalStale) Error() string {
	return fmt.Sprintf(
		"githubintegration: %s#%d head moved from %s to %s since this review was proposed; re-hydrate and re-propose before submitting",
		e.Repository, e.PullRequestNumber, e.ProposedHeadSHA, e.CurrentHeadSHA,
	)
}

// HydratePullRequest returns complete pull request context: normalized PR
// metadata, every page of changed files/comments/unresolved review
// threads, the newer Check Runs API's state, and (when the PR has a head
// SHA) the older legacy Commit Status API's state - a PR's true CI
// state can depend on either depending on which integration posted it.
// "complete" in the returned map is true only if every one of those
// paginated reads reported complete=true itself; a caller that needs to
// know whether it saw the full picture should check that field rather
// than assume it.
func (s *Service) HydratePullRequest(ctx context.Context, runID, repository string, pullRequestNumber int) (map[string]any, error) {
	gate, err := s.registry.Gate(ctx, s.adapterIDFor(ctx, repository))
	if err != nil {
		return nil, fmt.Errorf("githubintegration: open github adapter: %w", err)
	}
	call := func(op string, params map[string]any) (map[string]any, error) {
		raw, err := gate.Call(ctx, runID, op, params)
		if err != nil {
			return nil, err
		}
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	}

	pr, err := call("github.getPullRequest", map[string]any{"repository": repository, "pullRequestNumber": pullRequestNumber})
	if err != nil {
		return nil, fmt.Errorf("githubintegration: fetch pull request: %w", err)
	}
	normalized, _ := pr["normalized"].(map[string]any)
	headSHA, _ := normalized["headSha"].(string)

	files, err := call("github.getPullRequestFiles", map[string]any{"repository": repository, "pullRequestNumber": pullRequestNumber})
	if err != nil {
		return nil, fmt.Errorf("githubintegration: fetch pull request files: %w", err)
	}
	comments, err := call("github.listPullRequestComments", map[string]any{"repository": repository, "pullRequestNumber": pullRequestNumber})
	if err != nil {
		return nil, fmt.Errorf("githubintegration: fetch pull request comments: %w", err)
	}
	threads, err := call("github.listUnresolvedReviewThreads", map[string]any{"repository": repository, "pullRequestNumber": pullRequestNumber})
	if err != nil {
		return nil, fmt.Errorf("githubintegration: fetch pull request review threads: %w", err)
	}

	out := map[string]any{"pull_request": pr, "files": files, "comments": comments, "unresolved_threads": threads}
	complete := pageComplete(files) && pageComplete(comments) && pageComplete(threads)

	if headSHA != "" {
		checks, err := call("github.getPullRequestChecks", map[string]any{"repository": repository, "ref": headSHA})
		if err != nil {
			return nil, fmt.Errorf("githubintegration: fetch pull request checks: %w", err)
		}
		out["checks"] = checks
		complete = complete && pageComplete(checks)

		legacyStatus, err := call("github.getCommitStatus", map[string]any{"repository": repository, "ref": headSHA})
		if err != nil {
			return nil, fmt.Errorf("githubintegration: fetch legacy commit status: %w", err)
		}
		out["legacy_commit_status"] = legacyStatus
	}

	out["complete"] = complete
	return out, nil
}

// pageComplete reports whether a call result's own page.complete field is
// true. A result with no page metadata at all (never paginated to begin
// with) counts as complete: there was only ever one page to see.
func pageComplete(result map[string]any) bool {
	page, ok := result["page"].(map[string]any)
	if !ok {
		return true
	}
	complete, ok := page["complete"].(bool)
	return !ok || complete
}

// currentHeadSHA refetches repository's pull request and returns its
// current head SHA, the one read SubmitReview needs to detect a stale
// proposal without paying for a full HydratePullRequest.
func (s *Service) currentHeadSHA(ctx context.Context, runID, repository string, pullRequestNumber int) (string, error) {
	gate, err := s.registry.Gate(ctx, s.adapterIDFor(ctx, repository))
	if err != nil {
		return "", fmt.Errorf("githubintegration: open github adapter: %w", err)
	}
	raw, err := gate.Call(ctx, runID, "github.getPullRequest", map[string]any{"repository": repository, "pullRequestNumber": pullRequestNumber})
	if err != nil {
		return "", fmt.Errorf("githubintegration: fetch pull request %s#%d: %w", repository, pullRequestNumber, err)
	}
	var result struct {
		Normalized struct {
			HeadSha string `json:"headSha"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("githubintegration: decode pull request %s#%d: %w", repository, pullRequestNumber, err)
	}
	return result.Normalized.HeadSha, nil
}

// CreatePullRequestRequest is what CreatePullRequest asks the GitHub
// adapter to open.
type CreatePullRequestRequest struct {
	RunID      string
	Repository string
	BaseBranch string
	HeadBranch string
	Title      string
	Body       string
}

// CreatePullRequest enqueues and synchronously resolves one
// github.createPullRequest intent, returning the pull request's number
// and URL. A retried call for the same repository/head/base resolves to
// the same durable intent instead of opening a second pull request, and -
// if that intent was left ambiguous by an earlier attempt -
// providerwrite.ReconcileGitHubCreatePR (registered against this exact
// operation) confirms whether it already applied before this call is
// ever allowed to retry it.
func (s *Service) CreatePullRequest(ctx context.Context, req CreatePullRequestRequest) (number int, url string, err error) {
	payload, err := json.Marshal(map[string]any{
		"repository": req.Repository, "baseBranch": req.BaseBranch, "headBranch": req.HeadBranch,
		"title": req.Title, "body": req.Body,
	})
	if err != nil {
		return 0, "", fmt.Errorf("githubintegration: encode github.createPullRequest payload for %s: %w", req.Repository, err)
	}
	resolved, err := providerwrite.ExecuteNow(ctx, s.outbox, s.registry, req.RunID, outbox.Intent{
		AdapterID: s.adapterIDFor(ctx, req.Repository), Operation: "github.createPullRequest", TargetKey: req.Repository,
		PayloadJSON:          string(payload),
		OperationFingerprint: providerwrite.GitHubCreatePRFingerprint(req.Repository, req.HeadBranch, req.BaseBranch),
	})
	if err != nil {
		return 0, "", fmt.Errorf("githubintegration: enqueue github.createPullRequest for %s: %w", req.Repository, err)
	}
	if resolved.Status != outbox.StatusSucceeded {
		reason := resolved.LastErrorRedacted
		if reason == "" {
			reason = fmt.Sprintf("intent ended in status %q", resolved.Status)
		}
		return 0, "", fmt.Errorf("githubintegration: github.createPullRequest for %s did not succeed: %s", req.Repository, reason)
	}
	fmt.Sscanf(resolved.ExternalID, "%d", &number)
	effects, err := s.outbox.ListEffects(ctx, resolved.ID)
	if err != nil {
		return 0, "", fmt.Errorf("githubintegration: read github.createPullRequest effects for %s: %w", req.Repository, err)
	}
	for _, effect := range effects {
		if effect.EffectKey == "url" {
			url = effect.ExternalID
		}
	}
	return number, url, nil
}

// SubmitReviewRequest is what SubmitReview asks the GitHub adapter to
// submit. HeadSHA is the commit the review's findings were computed
// against - the exact value SubmitReview checks freshness against and
// records for ReconcileGitHubReview to match on later.
type SubmitReviewRequest struct {
	RunID             string
	Repository        string
	PullRequestNumber int
	HeadSHA           string
	Body              string
	Event             string
	Comments          []map[string]any
	// ReviewID is the caller's own durable review proposal id (e.g.
	// delivery.GitHubPRReview.ID), folded into the fingerprint so two
	// separate proposals against the same PR/SHA never collide.
	ReviewID string
}

// SubmitReview refetches the pull request and compares its current head
// against req.HeadSHA before ever enqueueing anything: a mismatch means
// the diff this review's findings describe no longer exists, and returns
// *ErrReviewProposalStale instead of submitting a review against a stale
// proposal. Otherwise it enqueues and synchronously resolves one
// github.createPullRequestReview intent - failing short of success (a
// retryable rejection, an unresolved ambiguous attempt) cancels the
// intent rather than leaving it to retry silently in the background, so
// this call always returns one definitive outcome.
func (s *Service) SubmitReview(ctx context.Context, req SubmitReviewRequest) (externalID string, err error) {
	currentHeadSHA, err := s.currentHeadSHA(ctx, req.RunID, req.Repository, req.PullRequestNumber)
	if err != nil {
		return "", err
	}
	if currentHeadSHA != "" && currentHeadSHA != req.HeadSHA {
		return "", &ErrReviewProposalStale{
			Repository: req.Repository, PullRequestNumber: req.PullRequestNumber,
			ProposedHeadSHA: req.HeadSHA, CurrentHeadSHA: currentHeadSHA,
		}
	}

	payload, err := json.Marshal(map[string]any{
		"pull_request_number": req.PullRequestNumber,
		"head_sha":            req.HeadSHA,
		"body":                req.Body,
		"event":               req.Event,
		"comments":            req.Comments,
	})
	if err != nil {
		return "", fmt.Errorf("githubintegration: encode github pull request review payload: %w", err)
	}
	resolved, err := providerwrite.ExecuteNow(ctx, s.outbox, s.registry, req.RunID, outbox.Intent{
		AdapterID: s.adapterIDFor(ctx, req.Repository), Operation: "github.createPullRequestReview", TargetKey: req.Repository,
		PayloadJSON:          string(payload),
		OperationFingerprint: providerwrite.GitHubReviewFingerprint(req.Repository, req.PullRequestNumber, req.HeadSHA, req.ReviewID),
	})
	if err != nil {
		return "", fmt.Errorf("githubintegration: enqueue github pull request review: %w", err)
	}
	if resolved.Status != outbox.StatusSucceeded {
		reason := resolved.LastErrorRedacted
		if reason == "" {
			reason = fmt.Sprintf("intent ended in status %q", resolved.Status)
		}
		if _, cancelErr := s.outbox.Cancel(ctx, resolved.ID, "githubintegration: giving up after one synchronous submission attempt"); cancelErr != nil {
			return "", fmt.Errorf("githubintegration: cancel unresolved github pull request review intent: %w", cancelErr)
		}
		return "", fmt.Errorf("githubintegration: github pull request review did not succeed: %s", reason)
	}
	return resolved.ExternalID, nil
}

// adapterIDFor names the adapter process that speaks for the organisation
// a repository belongs to.
//
// A repository is always written "owner/repo", and the owner used to be
// taken as the organisation directly. That is right only while every
// organisation is named after its owner: a credential configured from one
// site holds an account of whatever name the token belongs to, and a
// repository under that account resolved to an adapter id no credential
// answered for. With a resolver wired the owner is looked up first; with
// none, or with nothing configured for it, the old derivation stands. A
// repository given without an owner keeps using the bare "github" adapter.
func (s *Service) adapterIDFor(ctx context.Context, repository string) string {
	owner, _, found := strings.Cut(strings.TrimSpace(repository), "/")
	if !found {
		return "github"
	}
	if s.orgs != nil {
		if org, ok := s.orgs(ctx, repository); ok {
			return adapters.QualifyAdapterID("github", providercreds.NormalizeOrgID(org))
		}
	}
	return adapters.QualifyAdapterID("github", providercreds.NormalizeOrgID(owner))
}
