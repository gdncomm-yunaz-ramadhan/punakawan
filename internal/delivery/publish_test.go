package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakePRProvider is a PRProvider test double that records how many times
// Publish was actually called, so idempotent-resume tests can assert the
// provider was never invoked a second time.
type fakePRProvider struct {
	calls  int
	number int
	url    string
	err    error
}

func (f *fakePRProvider) Publish(ctx context.Context, req PublishPRRequest) (int, string, error) {
	f.calls++
	if f.err != nil {
		return 0, "", f.err
	}
	return f.number, f.url, nil
}

func (f *fakePRProvider) ProviderName() protocol.DeliveryLanePrProvider {
	return protocol.DeliveryLanePrProviderGithub
}

func TestPublishPullRequestHappyPath(t *testing.T) {
	s, orchID, laneID, token := leasedLaneForRoleStages(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	provider := &fakePRProvider{number: 42, url: "https://example.com/pr/42"}
	req := PublishPRRequest{RepoSlug: "acme/widgets", BaseBranch: "main", HeadBranch: "punakawan/widgets", Title: "Widgets", Body: "body"}

	published, err := s.PublishPullRequest(ctx, "pub-1", orchID, laneID, token, provider, req, lane.Revision)
	if err != nil {
		t.Fatalf("PublishPullRequest: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider called once, got %d", provider.calls)
	}
	if published.PrNumber == nil || *published.PrNumber != 42 {
		t.Fatalf("expected pr_number 42, got %+v", published.PrNumber)
	}
	if published.PrUrl == nil || *published.PrUrl != "https://example.com/pr/42" {
		t.Fatalf("expected pr_url to round-trip, got %+v", published.PrUrl)
	}
	if published.PrRepoSlug == nil || *published.PrRepoSlug != "acme/widgets" {
		t.Fatalf("expected pr_repo_slug to round-trip, got %+v", published.PrRepoSlug)
	}
}

func TestPublishPullRequestIdempotentResumeDoesNotCallProviderAgain(t *testing.T) {
	s, orchID, laneID, token := leasedLaneForRoleStages(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	provider := &fakePRProvider{number: 7, url: "https://example.com/pr/7"}
	req := PublishPRRequest{RepoSlug: "acme/widgets", BaseBranch: "main", HeadBranch: "punakawan/widgets", Title: "Widgets", Body: "body"}

	first, err := s.PublishPullRequest(ctx, "pub-1", orchID, laneID, token, provider, req, lane.Revision)
	if err != nil {
		t.Fatalf("PublishPullRequest (first): %v", err)
	}

	// A crash-and-retry presents a DIFFERENT idempotency key, simulating a
	// fresh process. The domain-level pr_number check, not the audit-log
	// idempotency mechanism, must be what stops a second pull request here.
	second, err := s.PublishPullRequest(ctx, "pub-2-different-key", orchID, laneID, token, provider, req, first.Revision)
	if err != nil {
		t.Fatalf("PublishPullRequest (resume): %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider called exactly once across both calls, got %d", provider.calls)
	}
	if second.PrNumber == nil || *second.PrNumber != 7 {
		t.Fatalf("expected the same pr_number to be returned unchanged, got %+v", second.PrNumber)
	}
	if second.PrUrl == nil || *second.PrUrl != "https://example.com/pr/7" {
		t.Fatalf("expected the same pr_url to be returned unchanged, got %+v", second.PrUrl)
	}
}

func TestPublishPullRequestRejectsLeaseTokenMismatch(t *testing.T) {
	s, orchID, laneID, _ := leasedLaneForRoleStages(t)
	ctx := context.Background()
	lane, err := s.GetLane(ctx, orchID, laneID)
	if err != nil {
		t.Fatalf("GetLane: %v", err)
	}

	provider := &fakePRProvider{number: 1, url: "https://example.com/pr/1"}
	req := PublishPRRequest{RepoSlug: "acme/widgets", BaseBranch: "main", HeadBranch: "punakawan/widgets"}

	if _, err := s.PublishPullRequest(ctx, "pub-1", orchID, laneID, "wrong-token", provider, req, lane.Revision); !errors.Is(err, ErrLeaseTokenMismatch) {
		t.Fatalf("expected ErrLeaseTokenMismatch, got %v", err)
	}
	if provider.calls != 0 {
		t.Fatalf("expected provider never called on a rejected publish, got %d calls", provider.calls)
	}
}
