package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

const proposalBasePlanContent = "# Plan\n\n## Security\n\nBinds to loopback only.\n"
const proposalRevisedPlanContent = "# Plan\n\n## Security\n\nBinds to loopback only by default, with an optional authenticated LAN mode.\n"

// seedQueuedReviewWithComment builds a plan + a review already past
// draft (as if Submit had run) with one open comment, ready for a
// proposal to be posted against it.
func seedQueuedReviewWithComment(t *testing.T) (reviewID string, plans *artifact.PlanStore, reviews *artifact.ReviewStore) {
	t.Helper()
	root := t.TempDir()
	plans = &artifact.PlanStore{WorkspaceRoot: root}
	reviews = &artifact.ReviewStore{WorkspaceRoot: root}
	ref := seedPlan(t, plans, "plan-panel", proposalBasePlanContent)

	reviewID = "review-1"
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: reviewID, WorkspaceId: "punakawan", Status: protocol.ArtifactReviewMetadataStatusQueued},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: ref.Version, RevisionHash: ref.RevisionHash},
		Review:   protocol.ArtifactReviewReview{Title: "Panel review"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}
	if err := reviews.AppendComment(reviewID, protocol.ArtifactComment{
		Id: "c-1", ReviewId: reviewID, Status: protocol.ArtifactCommentStatusOpen,
		Anchor: protocol.ArtifactCommentAnchor{Kind: protocol.ArtifactCommentAnchorKindMarkdownBlock, BaseRevisionHash: ref.RevisionHash},
		Body:   "Add an authenticated LAN mode.",
	}); err != nil {
		t.Fatalf("AppendComment: %v", err)
	}
	return reviewID, plans, reviews
}

func resolvedCreateProposalBody(commentStatus protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatus) []byte {
	explanation := "Added an optional authenticated LAN mode section."
	body, _ := json.Marshal(createProposalRequest{
		Content:       proposalRevisedPlanContent,
		ChangeSummary: "Added authenticated LAN mode",
		CommentResolutions: []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem{
			{CommentId: "c-1", Status: commentStatus, Explanation: &explanation, ChangedBlockIds: []string{"panel.security"}},
		},
	})
	return body
}

func TestCreateProposalHandlerStoresAPassingProposal(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)

	body := resolvedCreateProposalBody(protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CreateProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var resp createProposalResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Structural.Passed || !resp.Compliance.Passed {
		t.Fatalf("resp = %+v, want both reports to pass", resp)
	}
	if resp.Proposal.Metadata.Attempt != 1 {
		t.Fatalf("Attempt = %d, want 1", resp.Proposal.Metadata.Attempt)
	}

	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusProposalReady {
		t.Fatalf("review status = %q, want proposal_ready", review.Metadata.Status)
	}
}

func TestCreateProposalHandlerFlagsAnUnresolvedComment(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)

	body, _ := json.Marshal(createProposalRequest{Content: proposalRevisedPlanContent})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CreateProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (a failing validation is still stored): %s", rec.Code, rec.Body)
	}
	var resp createProposalResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Compliance.Passed {
		t.Fatal("Compliance.Passed = true, want false for an unresolved comment")
	}
}

func TestCreateProposalHandlerRejectsADraftReview(t *testing.T) {
	root := t.TempDir()
	plans := &artifact.PlanStore{WorkspaceRoot: root}
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	ref := seedPlan(t, plans, "plan-panel", proposalBasePlanContent)
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: "review-1", Status: protocol.ArtifactReviewMetadataStatusDraft},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: ref.Version, RevisionHash: ref.RevisionHash},
		Review:   protocol.ArtifactReviewReview{Title: "t"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	body, _ := json.Marshal(createProposalRequest{Content: proposalRevisedPlanContent})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/proposals", bytes.NewReader(body))
	req.SetPathValue("reviewId", "review-1")
	rec := httptest.NewRecorder()
	CreateProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func createProposal(t *testing.T, reviews *artifact.ReviewStore, plans *artifact.PlanStore, reviewID string) createProposalResponse {
	t.Helper()
	body := resolvedCreateProposalBody(protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CreateProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("createProposal status = %d: %s", rec.Code, rec.Body)
	}
	var resp createProposalResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestProposalHandlerReturnsStoredProposal(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	created := createProposal(t, reviews, plans, reviewID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/proposals/1", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	ProposalHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got protocol.ArtifactRevisionProposal
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Metadata.Id != created.Proposal.Metadata.Id {
		t.Fatalf("got.Metadata.Id = %q, want %q", got.Metadata.Id, created.Proposal.Metadata.Id)
	}
}

func TestProposalDiffHandlerReportsAddedAndRemovedLines(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/proposals/1/diff", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	ProposalDiffHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var resp diffResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Summary.Added == 0 && resp.Summary.Removed == 0 {
		t.Fatalf("summary = %+v, want a nonzero diff between base and revised content", resp.Summary)
	}
}

func TestProposalValidationHandlerRecomputesLiveReports(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/proposals/1/validation", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	ProposalValidationHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var resp validationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Structural.Passed || !resp.Compliance.Passed {
		t.Fatalf("resp = %+v, want both to pass", resp)
	}
}

func TestAcceptProposalHandlerCreatesANewCanonicalVersion(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/accept", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	AcceptProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	current, err := plans.Current("plan-panel")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.Version != 2 {
		t.Fatalf("current.Version = %d, want 2 (accepted proposal became the new canonical version)", current.Version)
	}
	content, _, err := plans.Version("plan-panel", 2)
	if err != nil {
		t.Fatalf("Version(2): %v", err)
	}
	if string(content) != proposalRevisedPlanContent {
		t.Fatalf("content = %q, want the accepted proposal's content", content)
	}

	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusAccepted {
		t.Fatalf("review status = %q, want accepted", review.Metadata.Status)
	}
}

func TestAcceptProposalHandlerDetectsConflict(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)

	// The canonical plan changes out from under the review after the
	// proposal was generated - acceptance must refuse, not silently
	// overwrite the newer version.
	if _, err := plans.CreateVersion("plan-panel", "punakawan", []byte("# Plan\n\nSomeone else changed this.\n"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/accept", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	AcceptProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusConflicted {
		t.Fatalf("review status = %q, want conflicted", review.Metadata.Status)
	}
}

func TestAcceptProposalHandlerRefusesAFailedValidation(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	// No comment_resolutions supplied - compliance fails, so acceptance
	// must be refused even though nothing about the canonical hash
	// conflicts.
	body, _ := json.Marshal(createProposalRequest{Content: proposalRevisedPlanContent})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CreateProposalHandler(reviews, ArtifactStores{Plans: plans})(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("createProposal status = %d: %s", rec.Code, rec.Body)
	}

	acceptReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/accept", nil)
	acceptReq.SetPathValue("reviewId", reviewID)
	acceptReq.SetPathValue("proposalId", "1")
	acceptRec := httptest.NewRecorder()
	AcceptProposalHandler(reviews, ArtifactStores{Plans: plans})(acceptRec, acceptReq)

	if acceptRec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", acceptRec.Code)
	}
}

func TestRejectProposalHandlerNeverTouchesCanonicalContent(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/reject", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	RejectProposalHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	current, err := plans.Current("plan-panel")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.Version != 1 {
		t.Fatalf("current.Version = %d, want 1 (rejection must not create a new version)", current.Version)
	}
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusRejected {
		t.Fatalf("review status = %q, want rejected", review.Metadata.Status)
	}
}

func TestRequestChangesHandlerDispatchesANewAttempt(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)

	body, _ := json.Marshal(requestChangesRequest{Instruction: "Please also cover the mobile client."})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/request-changes", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("proposalId", "1")
	rec := httptest.NewRecorder()
	RequestChangesHandler(reviews, &stubDispatcher{})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusRevisionRequested {
		t.Fatalf("review status = %q, want revision_requested", review.Metadata.Status)
	}
}

func TestRebaseHandlerRepointsAConflictedReviewAtTheLatestVersion(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)
	acceptReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/accept", nil)
	acceptReq.SetPathValue("reviewId", reviewID)
	acceptReq.SetPathValue("proposalId", "1")
	AcceptProposalHandler(reviews, ArtifactStores{Plans: plans})(httptest.NewRecorder(), acceptReq)

	// Simulate a second, now-conflicted review still pinned to version 1.
	reviews2 := reviews
	if err := reviews2.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: "review-2", Status: protocol.ArtifactReviewMetadataStatusConflicted},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: 1, RevisionHash: artifact.Hash([]byte(proposalBasePlanContent))},
		Review:   protocol.ArtifactReviewReview{Title: "Second review"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-2/rebase", nil)
	req.SetPathValue("reviewId", "review-2")
	rec := httptest.NewRecorder()
	RebaseHandler(reviews, ArtifactStores{Plans: plans})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	rebased, err := reviews.GetReview("review-2")
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if rebased.Artifact.Version != 2 || rebased.Metadata.Status != protocol.ArtifactReviewMetadataStatusDraft {
		t.Fatalf("rebased = %+v, want version 2 and status draft", rebased)
	}
}

func TestListProposalsHandlerListsAllAttempts(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	createProposal(t, reviews, plans, reviewID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/proposals", nil)
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	ListProposalsHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out struct {
		Items []protocol.ArtifactRevisionProposal `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Items))
	}
}

func TestListProposalsHandlerReturnsEmptyForNoProposals(t *testing.T) {
	reviewID, _, reviews := seedQueuedReviewWithComment(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/proposals", nil)
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	ListProposalsHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out struct {
		Items []protocol.ArtifactRevisionProposal `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(out.Items))
	}
}
