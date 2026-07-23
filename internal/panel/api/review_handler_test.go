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

func seedPlan(t *testing.T, plans *artifact.PlanStore, planID string, content string) protocol.ArtifactReference {
	t.Helper()
	ref, err := plans.CreateVersion(planID, "punakawan", []byte(content), time.Now())
	if err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	return ref
}

func TestCreateReviewHandlerOpensADraftPinnedToTheCurrentVersion(t *testing.T) {
	root := t.TempDir()
	plans := &artifact.PlanStore{WorkspaceRoot: root}
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	ref := seedPlan(t, plans, "plan-panel", "# Plan\n\nBody.\n")

	body, _ := json.Marshal(createReviewRequest{Title: "Panel architecture revision"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/plan/plan-panel/reviews", bytes.NewReader(body))
	req.SetPathValue("type", "plan")
	req.SetPathValue("id", "plan-panel")
	rec := httptest.NewRecorder()
	CreateReviewHandler(plans, reviews, "punakawan")(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var got protocol.ArtifactReview
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Artifact.Version != ref.Version || got.Artifact.RevisionHash != ref.RevisionHash {
		t.Fatalf("Artifact = %+v, want it pinned to %+v", got.Artifact, ref)
	}
	if got.Metadata.Status != protocol.ArtifactReviewMetadataStatusDraft {
		t.Fatalf("Status = %q, want draft", got.Metadata.Status)
	}
}

func TestCreateReviewHandlerRejectsUnknownArtifactType(t *testing.T) {
	root := t.TempDir()
	plans := &artifact.PlanStore{WorkspaceRoot: root}
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}

	body, _ := json.Marshal(createReviewRequest{Title: "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/retrieval_recipe/r-1/reviews", bytes.NewReader(body))
	req.SetPathValue("type", "retrieval_recipe")
	req.SetPathValue("id", "r-1")
	rec := httptest.NewRecorder()
	CreateReviewHandler(plans, reviews, "punakawan")(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateReviewHandlerRequiresATitle(t *testing.T) {
	root := t.TempDir()
	plans := &artifact.PlanStore{WorkspaceRoot: root}
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	seedPlan(t, plans, "plan-panel", "# Plan\n")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/plan/plan-panel/reviews", bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("type", "plan")
	req.SetPathValue("id", "plan-panel")
	rec := httptest.NewRecorder()
	CreateReviewHandler(plans, reviews, "punakawan")(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateReviewHandlerReturns404ForUnknownPlan(t *testing.T) {
	root := t.TempDir()
	plans := &artifact.PlanStore{WorkspaceRoot: root}
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}

	body, _ := json.Marshal(createReviewRequest{Title: "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/plan/no-such-plan/reviews", bytes.NewReader(body))
	req.SetPathValue("type", "plan")
	req.SetPathValue("id", "no-such-plan")
	rec := httptest.NewRecorder()
	CreateReviewHandler(plans, reviews, "punakawan")(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestReviewHandlerReturnsTheStoredReview(t *testing.T) {
	root := t.TempDir()
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: "review-1", Status: protocol.ArtifactReviewMetadataStatusDraft},
		Review:   protocol.ArtifactReviewReview{Title: "Title"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/review-1", nil)
	req.SetPathValue("reviewId", "review-1")
	rec := httptest.NewRecorder()
	ReviewHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
}

func TestReviewHandlerReturns404ForUnknownReview(t *testing.T) {
	reviews := &artifact.ReviewStore{WorkspaceRoot: t.TempDir()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/no-such-review", nil)
	req.SetPathValue("reviewId", "no-such-review")
	rec := httptest.NewRecorder()
	ReviewHandler(reviews)(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateReviewHandlerEditsTitleAndInstruction(t *testing.T) {
	root := t.TempDir()
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: "review-1", Status: protocol.ArtifactReviewMetadataStatusDraft},
		Review:   protocol.ArtifactReviewReview{Title: "Old title"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	newTitle := "New title"
	instruction := "Please focus on the security section."
	body, _ := json.Marshal(updateReviewRequest{Title: &newTitle, Instruction: &instruction})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/reviews/review-1", bytes.NewReader(body))
	req.SetPathValue("reviewId", "review-1")
	rec := httptest.NewRecorder()
	UpdateReviewHandler(reviews)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	got, err := reviews.GetReview("review-1")
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if got.Review.Title != newTitle || got.Review.Instruction == nil || *got.Review.Instruction != instruction {
		t.Fatalf("Review = %+v, want title/instruction updated", got.Review)
	}
	if got.Metadata.UpdatedAt == nil {
		t.Fatal("UpdatedAt not set after edit")
	}
}

func TestUpdateReviewHandlerRejectsEditingANonDraftReview(t *testing.T) {
	root := t.TempDir()
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	if err := reviews.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: "review-1", Status: protocol.ArtifactReviewMetadataStatusSubmitted},
		Review:   protocol.ArtifactReviewReview{Title: "Title"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	newTitle := "New title"
	body, _ := json.Marshal(updateReviewRequest{Title: &newTitle})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/reviews/review-1", bytes.NewReader(body))
	req.SetPathValue("reviewId", "review-1")
	rec := httptest.NewRecorder()
	UpdateReviewHandler(reviews)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}
