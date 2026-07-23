package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// seedTwoReviewsOnSamePlanVersion opens two independent reviews against
// the exact same artifact/version, mirroring two people (or two browser
// tabs) reviewing the same plan simultaneously - the setup for every test
// in this file.
func seedTwoReviewsOnSamePlanVersion(t *testing.T) (planID string, plans *artifact.PlanStore, reviews *artifact.ReviewStore, reviewA, reviewB string) {
	t.Helper()
	root := t.TempDir()
	plans = &artifact.PlanStore{WorkspaceRoot: root}
	reviews = &artifact.ReviewStore{WorkspaceRoot: root}
	planID = "plan-panel"
	ref := seedPlan(t, plans, planID, proposalBasePlanContent)

	reviewA, reviewB = "review-a", "review-b"
	for _, id := range []string{reviewA, reviewB} {
		if err := reviews.PutReview(protocol.ArtifactReview{
			Metadata: protocol.ArtifactReviewMetadata{Id: id, WorkspaceId: "punakawan", Status: protocol.ArtifactReviewMetadataStatusQueued},
			Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: planID, Version: ref.Version, RevisionHash: ref.RevisionHash},
			Review:   protocol.ArtifactReviewReview{Title: "Review of " + id},
		}); err != nil {
			t.Fatalf("PutReview(%s): %v", id, err)
		}
		if err := reviews.AppendComment(id, protocol.ArtifactComment{
			Id: id + "-c1", ReviewId: id, Status: protocol.ArtifactCommentStatusOpen,
			Anchor: protocol.ArtifactCommentAnchor{Kind: protocol.ArtifactCommentAnchorKindMarkdownBlock, BaseRevisionHash: ref.RevisionHash},
			Body:   "Please revise.",
		}); err != nil {
			t.Fatalf("AppendComment(%s): %v", id, err)
		}
	}
	return planID, plans, reviews, reviewA, reviewB
}

func postProposal(t *testing.T, reviews *artifact.ReviewStore, plans *artifact.PlanStore, reviewID, content string) createProposalResponse {
	t.Helper()
	explanation := "done"
	body, _ := json.Marshal(createProposalRequest{
		Content:       content,
		ChangeSummary: "revision for " + reviewID,
		CommentResolutions: []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem{
			{CommentId: reviewID + "-c1", Status: protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed, Explanation: &explanation, ChangedBlockIds: []string{"panel.security"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CreateProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("postProposal(%s) status = %d, want 201: %s", reviewID, rec.Code, rec.Body)
	}
	var resp createProposalResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func acceptProposal(reviews *artifact.ReviewStore, plans *artifact.PlanStore, reviewID string, attempt int) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/accept", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", strconv.Itoa(attempt))
	rec := httptest.NewRecorder()
	AcceptProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)
	return rec
}

// TestTwoConcurrentReviewsOnSameVersionOneAcceptFlipsTheOtherToConflicted
// covers §12's optimistic-concurrency rule under true concurrent access:
// two reviews are opened against the same artifact version at the same
// time, both produce a proposal, and then both attempt to accept "at
// once" (modeled here as A completing first, which is the only order that
// can happen given the store has no cross-review lock - the test's job is
// to confirm B's subsequent accept correctly detects the now-stale base
// rather than corrupting state or double-creating a version).
func TestTwoConcurrentReviewsOnSameVersionOneAcceptFlipsTheOtherToConflicted(t *testing.T) {
	_, plans, reviews, reviewA, reviewB := seedTwoReviewsOnSamePlanVersion(t)

	postProposal(t, reviews, plans, reviewA, "# Plan\n\n## Security\n\nA's revision.\n")
	postProposal(t, reviews, plans, reviewB, "# Plan\n\n## Security\n\nB's revision.\n")

	// Both accepts are launched concurrently; whichever the runtime
	// schedules first will see current==base and succeed, the other must
	// observe the just-created new version and refuse.
	var wg sync.WaitGroup
	var recA, recB *httptest.ResponseRecorder
	wg.Add(2)
	go func() { defer wg.Done(); recA = acceptProposal(reviews, plans, reviewA, 1) }()
	go func() { defer wg.Done(); recB = acceptProposal(reviews, plans, reviewB, 1) }()
	wg.Wait()

	codes := []int{recA.Code, recB.Code}
	successes, conflicts := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected code %d in %v, want only 200/409 combinations", c, codes)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("codes = %v, want exactly one 200 and one 409 (both racing to accept the same base version)", codes)
	}

	// Canonical history must show exactly one new version was created -
	// concurrent acceptance must never produce two version-2s or corrupt
	// current.yaml into an inconsistent state.
	current, err := plans.Current("plan-panel")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.Version != 2 {
		t.Fatalf("current.Version = %d, want exactly 2 (only one accept may have won)", current.Version)
	}

	loserID := reviewA
	if recA.Code == http.StatusOK {
		loserID = reviewB
	}
	loser, err := reviews.GetReview(loserID)
	if err != nil {
		t.Fatalf("GetReview(%s): %v", loserID, err)
	}
	if loser.Metadata.Status != protocol.ArtifactReviewMetadataStatusConflicted {
		t.Fatalf("loser review status = %q, want conflicted", loser.Metadata.Status)
	}

	winnerID := reviewB
	if recA.Code == http.StatusOK {
		winnerID = reviewA
	}
	winner, err := reviews.GetReview(winnerID)
	if err != nil {
		t.Fatalf("GetReview(%s): %v", winnerID, err)
	}
	if winner.Metadata.Status != protocol.ArtifactReviewMetadataStatusAccepted {
		t.Fatalf("winner review status = %q, want accepted", winner.Metadata.Status)
	}
}

// TestConflictedReviewRebaseThenAcceptSucceedsAgainstNewBase extends
// AcceptProposalHandler's conflict path with the recovery half of §12: once
// a review has been flipped to conflicted by a concurrent accept, the
// existing RebaseHandler must let it re-anchor to the new canonical
// version and continue (rather than being permanently stuck), and a fresh
// proposal generated against the rebased version must then accept cleanly.
func TestConflictedReviewRebaseThenAcceptSucceedsAgainstNewBase(t *testing.T) {
	_, plans, reviews, reviewA, reviewB := seedTwoReviewsOnSamePlanVersion(t)
	postProposal(t, reviews, plans, reviewA, "# Plan\n\n## Security\n\nA's revision.\n")
	postProposal(t, reviews, plans, reviewB, "# Plan\n\n## Security\n\nB's revision.\n")

	if rec := acceptProposal(reviews, plans, reviewA, 1); rec.Code != http.StatusOK {
		t.Fatalf("accept A status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if rec := acceptProposal(reviews, plans, reviewB, 1); rec.Code != http.StatusConflict {
		t.Fatalf("accept B status = %d, want 409: %s", rec.Code, rec.Body)
	}

	rebaseReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewB+"/rebase", nil)
	rebaseReq.SetPathValue("reviewId", reviewB)
	rebaseRec := httptest.NewRecorder()
	RebaseHandler(reviews, ArtifactStores{Plans: plans})(rebaseRec, rebaseReq)
	if rebaseRec.Code != http.StatusOK {
		t.Fatalf("rebase status = %d, want 200: %s", rebaseRec.Code, rebaseRec.Body)
	}

	rebased, err := reviews.GetReview(reviewB)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if rebased.Metadata.Status != protocol.ArtifactReviewMetadataStatusDraft || rebased.Artifact.Version != 2 {
		t.Fatalf("rebased = %+v, want draft status pinned at version 2", rebased)
	}

	// Mirroring the real UX (§12: "the user reviews the refreshed
	// document and resubmits explicitly"), the rebased review must be
	// resubmitted before a new proposal can be posted against it -
	// CreateProposalHandler only accepts proposals for a review that has
	// moved past draft.
	if rec := submitReview(t, reviews, &stubDispatcher{}, reviewB); rec.Code != http.StatusCreated {
		t.Fatalf("resubmit after rebase status = %d, want 201: %s", rec.Code, rec.Body)
	}

	// A fresh proposal against the rebased base (attempt 2, since attempt
	// 1 already exists for review-b) must accept cleanly now.
	explanation := "done after rebase"
	body, _ := json.Marshal(createProposalRequest{
		Content:       "# Plan\n\n## Security\n\nB's revision, rebased.\n",
		ChangeSummary: "rebased revision",
		CommentResolutions: []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem{
			{CommentId: reviewB + "-c1", Status: protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed, Explanation: &explanation, ChangedBlockIds: []string{"panel.security"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewB+"/proposals", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewB)
	rec := httptest.NewRecorder()
	CreateProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("second proposal status = %d, want 201: %s", rec.Code, rec.Body)
	}

	acceptReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewB+"/proposals/2/accept", nil)
	acceptReq.SetPathValue("reviewId", reviewB)
	acceptReq.SetPathValue("proposalId", "2")
	acceptRec := httptest.NewRecorder()
	AcceptProposalHandler(reviews, ArtifactStores{Plans: plans})(acceptRec, acceptReq)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept after rebase status = %d, want 200: %s", acceptRec.Code, acceptRec.Body)
	}

	current, err := plans.Current("plan-panel")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.Version != 3 {
		t.Fatalf("current.Version = %d, want 3", current.Version)
	}
}

// TestAcceptProposalEndToEndStaleBaseVersionAfterUnrelatedMutation is the
// full end-to-end version of the stale-base-at-acceptance scenario: create
// a review, submit, obtain a proposal, then mutate the canonical plan out
// from under it via a *second, entirely unrelated* review's accept (not
// direct store manipulation), and confirm the original review's accept
// call gets 409 and flips to conflicted.
func TestAcceptProposalEndToEndStaleBaseVersionAfterUnrelatedMutation(t *testing.T) {
	root := t.TempDir()
	plans := &artifact.PlanStore{WorkspaceRoot: root}
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	ref := seedPlan(t, plans, "plan-panel", proposalBasePlanContent)

	reviewID := "review-1"
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: reviewID, WorkspaceId: "punakawan", Status: protocol.ArtifactReviewMetadataStatusDraft},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: ref.Version, RevisionHash: ref.RevisionHash},
		Review:   protocol.ArtifactReviewReview{Title: "Panel review"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}
	if err := reviews.AppendComment(reviewID, protocol.ArtifactComment{
		Id: "c-1", ReviewId: reviewID, Status: protocol.ArtifactCommentStatusOpen,
		Anchor: protocol.ArtifactCommentAnchor{Kind: protocol.ArtifactCommentAnchorKindMarkdownBlock, BaseRevisionHash: ref.RevisionHash},
		Body:   "Please add LAN mode.",
	}); err != nil {
		t.Fatalf("AppendComment: %v", err)
	}

	dispatcher := &stubDispatcher{}
	if rec := submitReview(t, reviews, dispatcher, reviewID); rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d, want 201: %s", rec.Code, rec.Body)
	}

	created := postProposal(t, reviews, plans, reviewID, proposalRevisedPlanContent)
	if created.Proposal.Metadata.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", created.Proposal.Metadata.Attempt)
	}

	// An entirely unrelated flow (not this review, not direct store
	// poking) mutates the canonical plan: a second review is created,
	// proposed, and accepted first.
	otherReviewID := "review-unrelated"
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: otherReviewID, WorkspaceId: "punakawan", Status: protocol.ArtifactReviewMetadataStatusQueued},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: ref.Version, RevisionHash: ref.RevisionHash},
		Review:   protocol.ArtifactReviewReview{Title: "Unrelated review"},
	}); err != nil {
		t.Fatalf("PutReview(other): %v", err)
	}
	if err := reviews.AppendComment(otherReviewID, protocol.ArtifactComment{
		Id: otherReviewID + "-c1", ReviewId: otherReviewID, Status: protocol.ArtifactCommentStatusOpen,
		Anchor: protocol.ArtifactCommentAnchor{Kind: protocol.ArtifactCommentAnchorKindMarkdownBlock, BaseRevisionHash: ref.RevisionHash},
		Body:   "Unrelated change.",
	}); err != nil {
		t.Fatalf("AppendComment(other): %v", err)
	}
	postProposal(t, reviews, plans, otherReviewID, "# Plan\n\n## Security\n\nSomeone else's unrelated change.\n")
	if rec := acceptProposal(reviews, plans, otherReviewID, 1); rec.Code != http.StatusOK {
		t.Fatalf("unrelated accept status = %d, want 200: %s", rec.Code, rec.Body)
	}

	// Now the original review's accept must observe the stale base.
	rec := acceptProposal(reviews, plans, reviewID, 1)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (base version gone stale): %s", rec.Code, rec.Body)
	}

	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusConflicted {
		t.Fatalf("review status = %q, want conflicted", review.Metadata.Status)
	}

	current, err := plans.Current("plan-panel")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.Version != 2 {
		t.Fatalf("current.Version = %d, want 2 (only the unrelated review's accept created a version)", current.Version)
	}
}
