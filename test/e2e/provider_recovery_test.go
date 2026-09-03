//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/deliveryservice"
	"github.com/ygrip/punakawan/internal/githubintegration"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/providerwrite"
)

// TestProviderRecoveryDedupesLostResponses simulates the exact failure
// this reliability rebuild exists to survive: a provider write's remote
// effect lands, but the response confirming it never reaches this
// process (a crash, a dropped connection, a client-side timeout) before
// the outbox durably records success. A worker that resumes afterward
// must never blindly replay the write - it reconciles by re-reading
// remote state first - so exactly one Jira comment and exactly one
// GitHub pull request ever exist, no matter how many times recovery
// runs.
func TestProviderRecoveryDedupesLostResponses(t *testing.T) {
	ctx := context.Background()
	s := newStack(t)

	s.registry.atlassianServer.addIssue("REC-1", "", "Investigate outage", "Root-cause the outage", "To Do", "Task")

	svc := s.deliveryService()
	jira := s.jiraService(defaultJiraWorkflowConfig())
	github := s.githubService()

	start, needInput, err := svc.StartOrResolve(ctx, deliveryservice.StartRequest{
		IdempotencyKey: "start-rec-1",
		Source:         &deliveryservice.SourceIdentity{Kind: deliveryservice.SourceJira, Provider: "jira", Tenant: "tenant-1", Key: "rec-1", Clarity: delivery.ClarityClear},
		Title:          "Investigate outage",
		HighLevelPlan:  deliveryservice.PlanDraft{Objective: "Investigate outage"},
		Session:        deliveryservice.SessionStart{Participant: "agent-1"},
	})
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needInput != nil {
		t.Fatalf("StartOrResolve needed input: %+v", needInput)
	}
	orchestrationID := start.Execution.OrchestrationID

	// Jira: the "delivery started" comment's remote write applies, but its
	// response is lost - the outbox marks the intent ambiguous instead of
	// treating the write as failed, and a later worker pass reconciles it
	// (finding the marker already posted) rather than posting a duplicate.
	s.registry.atlassian.loseResponseOnce("atlassian.addJiraComment")
	if err := jira.OnDeliveryStarted(ctx, orchestrationID); err != nil {
		t.Fatalf("OnDeliveryStarted: %v", err)
	}
	s.drainOutbox(t, 20)

	s.registry.atlassianServer.mu.Lock()
	commentCount := len(s.registry.atlassianServer.issues["REC-1"].comments)
	s.registry.atlassianServer.mu.Unlock()
	if commentCount != 1 {
		t.Fatalf("expected exactly one Jira comment to survive a lost response, got %d", commentCount)
	}

	commentIntent, err := s.outbox.GetByFingerprint(ctx, providerwrite.JiraCommentFingerprint(orchestrationID, "delivery.started", "REC-1"))
	if err != nil {
		t.Fatalf("GetByFingerprint(comment): %v", err)
	}
	if commentIntent.Status != outbox.StatusSucceeded {
		t.Fatalf("comment intent status = %q, want succeeded (reconciled, not replayed)", commentIntent.Status)
	}

	// GitHub: CreatePullRequest resolves its intent synchronously
	// (providerwrite.ExecuteNow), one attempt per call, never looping to
	// retry claiming on its own. So the first call - whose response is
	// lost right after the remote pull request is actually opened - sees
	// its intent left ambiguous and reports an error rather than a
	// fabricated success; a second call for the same repository/head/base
	// collapses onto that same durable intent and, finding it ambiguous,
	// reconciles by re-reading remote state (github.findPullRequest)
	// instead of opening a second pull request.
	prRequest := githubintegration.CreatePullRequestRequest{
		RunID: orchestrationID, Repository: "acme/outage-svc", BaseBranch: "main", HeadBranch: "lane/rec-1",
		Title: "Investigate outage", Body: "Closes REC-1",
	}
	s.registry.github.loseResponseOnce("github.createPullRequest")
	if _, _, err := github.CreatePullRequest(ctx, prRequest); err == nil {
		t.Fatalf("expected the first CreatePullRequest call (lost response) to report an error rather than a guessed success")
	}
	number, url, err := github.CreatePullRequest(ctx, prRequest)
	if err != nil {
		t.Fatalf("CreatePullRequest (reconciling retry): %v", err)
	}
	if number == 0 || url == "" {
		t.Fatalf("CreatePullRequest returned number=%d url=%q after reconciliation, want both resolved", number, url)
	}

	s.registry.githubServer.mu.Lock()
	prCount := len(s.registry.githubServer.prs)
	s.registry.githubServer.mu.Unlock()
	if prCount != 1 {
		t.Fatalf("expected exactly one pull request to survive a lost response, got %d", prCount)
	}

	prIntent, err := s.outbox.GetByFingerprint(ctx, providerwrite.GitHubCreatePRFingerprint("acme/outage-svc", "lane/rec-1", "main"))
	if err != nil {
		t.Fatalf("GetByFingerprint(pr): %v", err)
	}
	if prIntent.Status != outbox.StatusSucceeded {
		t.Fatalf("pull request intent status = %q, want succeeded (reconciled, not replayed)", prIntent.Status)
	}

	// A third call describing the exact same logical effect (the same
	// caller retrying yet again after already seeing a confirmed result)
	// must collapse onto the very same already-succeeded intent rather
	// than attempting the write again.
	number2, url2, err := github.CreatePullRequest(ctx, prRequest)
	if err != nil {
		t.Fatalf("CreatePullRequest (retry): %v", err)
	}
	if number2 != number || url2 != url {
		t.Fatalf("retry resolved to number=%d url=%q, want the same number=%d url=%q", number2, url2, number, url)
	}
	s.registry.githubServer.mu.Lock()
	prCountAfterRetry := len(s.registry.githubServer.prs)
	s.registry.githubServer.mu.Unlock()
	if prCountAfterRetry != 1 {
		t.Fatalf("expected the retried CreatePullRequest call to still resolve to exactly one pull request, got %d", prCountAfterRetry)
	}
}
