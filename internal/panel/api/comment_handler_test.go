package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

const commentTestPlanContent = "# Plan\n\n## Network Boundary\n\n<!-- pk:block:panel.security.loopback-default -->\nThe panel binds to loopback by default.\n"

func strp(s string) *string { return &s }

func seedDraftReviewWithComments(t *testing.T) (root, reviewID string, plans *artifact.PlanStore, reviews *artifact.ReviewStore) {
	t.Helper()
	root = t.TempDir()
	plans = &artifact.PlanStore{WorkspaceRoot: root}
	reviews = &artifact.ReviewStore{WorkspaceRoot: root}

	ref := seedPlan(t, plans, "plan-panel", commentTestPlanContent)
	reviewID = "review-1"
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: reviewID, Status: protocol.ArtifactReviewMetadataStatusDraft},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-panel", Version: ref.Version, RevisionHash: ref.RevisionHash},
		Review:   protocol.ArtifactReviewReview{Title: "Panel review"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}
	return root, reviewID, plans, reviews
}

func validAnchor(revisionHash string) protocol.ArtifactCommentAnchor {
	return protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: revisionHash,
		BlockId:          strp("panel.security.loopback-default"),
	}
}

func TestCreateCommentHandlerPersistsAResolvableComment(t *testing.T) {
	_, reviewID, plans, reviews := seedDraftReviewWithComments(t)
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}

	body, _ := json.Marshal(createCommentRequest{Anchor: validAnchor(review.Artifact.RevisionHash), Body: "Add an authenticated LAN mode."})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CreateCommentHandler(reviews, plans)(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var got protocol.ArtifactComment
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Id == "" || got.Status != protocol.ArtifactCommentStatusOpen {
		t.Fatalf("comment = %+v, want a generated id and open status", got)
	}
}

func TestCreateCommentHandlerRejectsAStaleBaseRevision(t *testing.T) {
	_, reviewID, plans, reviews := seedDraftReviewWithComments(t)

	body, _ := json.Marshal(createCommentRequest{Anchor: validAnchor("sha256:stale"), Body: "text"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CreateCommentHandler(reviews, plans)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestCreateCommentHandlerRejectsAnUnresolvableAnchor(t *testing.T) {
	_, reviewID, plans, reviews := seedDraftReviewWithComments(t)
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}

	anchor := protocol.ArtifactCommentAnchor{
		Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
		BaseRevisionHash: review.Artifact.RevisionHash,
		QuotedText:       strp("nothing like this exists anywhere in the document"),
	}
	body, _ := json.Marshal(createCommentRequest{Anchor: anchor, Body: "text"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CreateCommentHandler(reviews, plans)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
	}
}

func TestCreateCommentHandlerIsIdempotentForAClientSuppliedID(t *testing.T) {
	_, reviewID, plans, reviews := seedDraftReviewWithComments(t)
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}

	post := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(createCommentRequest{Id: "comment-fixed", Anchor: validAnchor(review.Artifact.RevisionHash), Body: "same text"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/review-1/comments", bytes.NewReader(body))
		req.SetPathValue("reviewId", reviewID)
		rec := httptest.NewRecorder()
		CreateCommentHandler(reviews, plans)(rec, req)
		return rec
	}

	if rec := post(); rec.Code != http.StatusCreated {
		t.Fatalf("first post status = %d, want 201: %s", rec.Code, rec.Body)
	}
	if rec := post(); rec.Code != http.StatusCreated {
		t.Fatalf("retried post status = %d, want 201: %s", rec.Code, rec.Body)
	}

	latest, err := reviews.LatestComments(reviewID)
	if err != nil {
		t.Fatalf("LatestComments: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("LatestComments = %d entries, want 1 (idempotent retry)", len(latest))
	}
}

func TestCommentsHandlerListsLatestComments(t *testing.T) {
	_, reviewID, _, reviews := seedDraftReviewWithComments(t)
	if err := reviews.AppendComment(reviewID, protocol.ArtifactComment{Id: "c-1", ReviewId: reviewID, Status: protocol.ArtifactCommentStatusOpen, Anchor: protocol.ArtifactCommentAnchor{Kind: protocol.ArtifactCommentAnchorKindMarkdownBlock, BaseRevisionHash: artifact.Hash([]byte("test"))}, Body: "first"}); err != nil {
		t.Fatalf("AppendComment: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/review-1/comments", nil)
	req.SetPathValue("reviewId", reviewID)
	rec := httptest.NewRecorder()
	CommentsHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var out struct {
		Items []protocol.ArtifactComment `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(out.Items))
	}
}

func TestUpdateCommentHandlerAppliesEdits(t *testing.T) {
	_, reviewID, _, reviews := seedDraftReviewWithComments(t)
	if err := reviews.AppendComment(reviewID, protocol.ArtifactComment{Id: "c-1", ReviewId: reviewID, Status: protocol.ArtifactCommentStatusOpen, Anchor: protocol.ArtifactCommentAnchor{Kind: protocol.ArtifactCommentAnchorKindMarkdownBlock, BaseRevisionHash: artifact.Hash([]byte("test"))}, Body: "first"}); err != nil {
		t.Fatalf("AppendComment: %v", err)
	}

	addressed := protocol.ArtifactCommentStatusAddressed
	body, _ := json.Marshal(updateCommentRequest{Status: &addressed})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/reviews/review-1/comments/c-1", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("commentId", "c-1")
	rec := httptest.NewRecorder()
	UpdateCommentHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	latest, err := reviews.LatestComments(reviewID)
	if err != nil {
		t.Fatalf("LatestComments: %v", err)
	}
	if len(latest) != 1 || latest[0].Status != protocol.ArtifactCommentStatusAddressed {
		t.Fatalf("latest = %+v, want status addressed", latest)
	}
}

func TestUpdateCommentHandlerReturns404ForUnknownComment(t *testing.T) {
	_, reviewID, _, reviews := seedDraftReviewWithComments(t)
	newBody := "edited"
	body, _ := json.Marshal(updateCommentRequest{Body: &newBody})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/reviews/review-1/comments/no-such-comment", bytes.NewReader(body))
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("commentId", "no-such-comment")
	rec := httptest.NewRecorder()
	UpdateCommentHandler(reviews)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteCommentHandlerMarksObsolete(t *testing.T) {
	_, reviewID, _, reviews := seedDraftReviewWithComments(t)
	if err := reviews.AppendComment(reviewID, protocol.ArtifactComment{Id: "c-1", ReviewId: reviewID, Status: protocol.ArtifactCommentStatusOpen, Anchor: protocol.ArtifactCommentAnchor{Kind: protocol.ArtifactCommentAnchorKindMarkdownBlock, BaseRevisionHash: artifact.Hash([]byte("test"))}, Body: "first"}); err != nil {
		t.Fatalf("AppendComment: %v", err)
	}

	del := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/reviews/review-1/comments/c-1", nil)
		req.SetPathValue("reviewId", reviewID)
		req.SetPathValue("commentId", "c-1")
		rec := httptest.NewRecorder()
		DeleteCommentHandler(reviews)(rec, req)
		return rec
	}

	if rec := del(); rec.Code != http.StatusNoContent {
		t.Fatalf("first delete status = %d, want 204", rec.Code)
	}
	// A second delete of the same (already-obsolete) comment must also
	// succeed - idempotent, per §14.
	if rec := del(); rec.Code != http.StatusNoContent {
		t.Fatalf("second delete status = %d, want 204", rec.Code)
	}

	latest, err := reviews.LatestComments(reviewID)
	if err != nil {
		t.Fatalf("LatestComments: %v", err)
	}
	if len(latest) != 1 || latest[0].Status != protocol.ArtifactCommentStatusObsolete {
		t.Fatalf("latest = %+v, want status obsolete", latest)
	}
}

func TestDeleteCommentHandlerOnUnknownCommentIsANoOp(t *testing.T) {
	_, reviewID, _, reviews := seedDraftReviewWithComments(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/reviews/review-1/comments/never-existed", nil)
	req.SetPathValue("reviewId", reviewID)
	req.SetPathValue("commentId", "never-existed")
	rec := httptest.NewRecorder()
	DeleteCommentHandler(reviews)(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}
