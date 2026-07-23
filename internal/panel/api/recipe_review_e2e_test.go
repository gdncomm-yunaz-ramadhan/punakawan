package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// requireDoltForRecipeE2E skips the retrieval_recipe end-to-end test when
// no dolt binary is available, mirroring internal/recipe's own
// newTestStore skip precedent - this test exercises the real Dolt-backed
// knowledge store, not a fake, since RecipeStore's whole point is
// wrapping that store faithfully.
func requireDoltForRecipeE2E(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
}

func newTestRecipeStore(t *testing.T) (*recipe.RecipeStore, *knowledge.Store) {
	t.Helper()
	requireDoltForRecipeE2E(t)

	dir := t.TempDir()
	sup := tools.New(dir)
	store, err := knowledge.Open(sup, filepath.Join(dir, "knowledge"))
	if err != nil {
		t.Fatalf("knowledge.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return &recipe.RecipeStore{Repo: &recipe.Repository{Store: store}}, store
}

// seedTestRecipe puts a minimal, valid retrieval-recipe record directly
// via the knowledge store (bypassing RecipeStore.CreateVersion, since
// there is no "previous version" for the very first one).
func seedTestRecipe(t *testing.T, store *knowledge.Store, id string) protocol.KnowledgeRecord {
	t.Helper()
	now := time.Now().UTC()
	version := 1
	rec := protocol.KnowledgeRecord{
		Id:     id,
		Type:   protocol.KnowledgeRecordTypeRetrievalRecipe,
		Status: "active",
		Title:  "Next sprint issues",
		Source: protocol.KnowledgeRecordSource{
			Provider:    "user_instruction",
			RetrievedAt: now,
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State:      protocol.KnowledgeRecordValidityStateVerified,
			VerifiedBy: []string{"user"},
		},
		RetrievalRecipe: &protocol.KnowledgeRecordRetrievalRecipe{
			Capability:    "jira.issue.search",
			Intent:        "project.next-sprint.issues",
			Provider:      "jira",
			Resource:      "issue",
			Operation:     "search",
			ReadOnly:      true,
			RecipeVersion: &version,
			Selector: protocol.KnowledgeRecordRetrievalRecipeSelector{
				All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
					{
						Field:    strp("project"),
						Operator: (*protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperator)(strp("equals")),
						Value:    map[string]interface{}{"literal": "AFF"},
					},
				},
			},
			Output: protocol.KnowledgeRecordRetrievalRecipeOutput{
				EntityType:    "jira_issue",
				IdentityField: "key",
				Fields:        []string{"key", "summary"},
			},
		},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("seed recipe Put: %v", err)
	}
	return rec
}

// TestRecipeArtifactReviewEndToEnd walks a retrieval_recipe artifact
// through the exact same review/proposal/acceptance HTTP protocol the
// plan-type tests exercise elsewhere in this package (see
// review_handler_test.go, comment_handler_test.go, submit_handler_test.go,
// proposal_handler_test.go): create review -> comment -> submit (stub
// dispatcher) -> create proposal -> diff -> accept. Per punokawan-q9r.6's
// explicit instruction, this is the SAME handlers and ReviewStore a plan
// review uses, only routed to a *recipe.RecipeStore instead of a
// *artifact.PlanStore - there is no second review state machine here.
func TestRecipeArtifactReviewEndToEnd(t *testing.T) {
	recipeStore, knowledgeStore := newTestRecipeStore(t)
	seeded := seedTestRecipe(t, knowledgeStore, "pkw:recipe/affiliate-api/jira-next-sprint")

	root := t.TempDir()
	reviews := &artifact.ReviewStore{WorkspaceRoot: root}
	stores := ArtifactStores{
		Recipes: func() (*recipe.RecipeStore, error) { return recipeStore, nil },
	}

	// 1. Create a review against the current (only) version.
	createBody, _ := json.Marshal(createReviewRequest{Title: "Tighten the project filter"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts/retrieval_recipe/"+seeded.Id+"/reviews", bytes.NewReader(createBody))
	createReq.SetPathValue("type", "retrieval_recipe")
	createReq.SetPathValue("id", seeded.Id)
	createRec := httptest.NewRecorder()
	CreateReviewHandler(stores, reviews, "punakawan")(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("CreateReviewHandler status = %d, want 201: %s", createRec.Code, createRec.Body)
	}
	var review protocol.ArtifactReview
	if err := json.Unmarshal(createRec.Body.Bytes(), &review); err != nil {
		t.Fatalf("decode review: %v", err)
	}
	if review.Artifact.Type != protocol.ArtifactReviewArtifactTypeRetrievalRecipe {
		t.Fatalf("Artifact.Type = %q, want retrieval_recipe", review.Artifact.Type)
	}
	reviewID := review.Metadata.Id

	// 2. Add a recipe_field_path comment anchored at the project filter's
	// literal value.
	fieldPath := "retrieval_recipe.selector.all.0.value.literal"
	commentBody, _ := json.Marshal(createCommentRequest{
		Anchor: protocol.ArtifactCommentAnchor{
			Kind:             protocol.ArtifactCommentAnchorKindRecipeFieldPath,
			BaseRevisionHash: review.Artifact.RevisionHash,
			FieldPath:        &fieldPath,
		},
		Body: "This should be AFFILIATE, not AFF - the project key changed.",
	})
	commentReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/comments", bytes.NewReader(commentBody))
	commentReq.SetPathValue("reviewId", reviewID)
	commentRec := httptest.NewRecorder()
	CreateCommentHandler(reviews, stores)(commentRec, commentReq)
	if commentRec.Code != http.StatusCreated {
		t.Fatalf("CreateCommentHandler status = %d, want 201: %s", commentRec.Code, commentRec.Body)
	}

	// 3. Submit, dispatching through a stub (no real BD/agent involved).
	dispatcher := &stubDispatcher{}
	submitReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/submit", nil)
	submitReq.SetPathValue("reviewId", reviewID)
	submitRec := httptest.NewRecorder()
	SubmitHandler(reviews, dispatcher)(submitRec, submitReq)
	if submitRec.Code != http.StatusCreated {
		t.Fatalf("SubmitHandler status = %d, want 201: %s", submitRec.Code, submitRec.Body)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("dispatcher.calls = %d, want 1", dispatcher.calls)
	}

	// 4. The revising agent reports back a corrected recipe record as the
	// proposal's content - a complete artifact, per §9, not a fragment.
	baseRecord, _, err := recipeStore.Version(seeded.Id, 1)
	if err != nil {
		t.Fatalf("recipeStore.Version: %v", err)
	}
	var proposedRecord protocol.KnowledgeRecord
	if err := json.Unmarshal(baseRecord, &proposedRecord); err != nil {
		t.Fatalf("decode base record: %v", err)
	}
	proposedRecord.RetrievalRecipe.Selector.All[0].Value = map[string]interface{}{"literal": "AFFILIATE"}

	explanation := "Updated the project key to AFFILIATE per the comment."
	proposalBody, _ := json.Marshal(createProposalRequest{
		Content:       mustMarshalIndent(t, proposedRecord),
		ChangeSummary: "Corrected the project key filter",
		CommentResolutions: []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem{
			// ChangedBlockIds' recipe-shaped equivalent is the field_path
			// that changed - ValidateReviewCompliance's
			// addressed_comments_identify_changes check is artifact-type
			// agnostic (it only checks the list is non-empty), so a
			// field_path satisfies it without needing a parallel check.
			{CommentId: mustFirstCommentID(t, reviews, reviewID), Status: protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed, Explanation: &explanation, ChangedBlockIds: []string{fieldPath}},
		},
	})
	proposalReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals", bytes.NewReader(proposalBody))
	proposalReq.SetPathValue("reviewId", reviewID)
	proposalRec := httptest.NewRecorder()
	CreateProposalHandler(reviews, stores)(proposalRec, proposalReq)
	if proposalRec.Code != http.StatusCreated {
		t.Fatalf("CreateProposalHandler status = %d, want 201: %s", proposalRec.Code, proposalRec.Body)
	}

	// 5. Diff the proposal against the base version.
	diffReq := httptest.NewRequest(http.MethodGet, "/api/v1/reviews/"+reviewID+"/proposals/1/diff", nil)
	diffReq.SetPathValue("reviewId", reviewID)
	diffReq.SetPathValue("proposalId", "1")
	diffRec := httptest.NewRecorder()
	ProposalDiffHandler(reviews, stores)(diffRec, diffReq)
	if diffRec.Code != http.StatusOK {
		t.Fatalf("ProposalDiffHandler status = %d, want 200: %s", diffRec.Code, diffRec.Body)
	}
	var diff diffResponse
	if err := json.Unmarshal(diffRec.Body.Bytes(), &diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if diff.Summary.Added == 0 && diff.Summary.Removed == 0 {
		t.Fatalf("diff summary = %+v, want a nonzero diff (AFF -> AFFILIATE)", diff.Summary)
	}

	// 6. Accept: this must create a NEW knowledge record (recipes version
	// by superseding, not by rewriting in place) and mark the old one
	// superseded.
	acceptReq := httptest.NewRequest(http.MethodPost, "/api/v1/reviews/"+reviewID+"/proposals/1/accept", nil)
	acceptReq.SetPathValue("reviewId", reviewID)
	acceptReq.SetPathValue("proposalId", "1")
	acceptRec := httptest.NewRecorder()
	AcceptProposalHandler(reviews, stores)(acceptRec, acceptReq)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("AcceptProposalHandler status = %d, want 200: %s", acceptRec.Code, acceptRec.Body)
	}

	newCurrent, err := recipeStore.Current(seeded.Id)
	if err != nil {
		t.Fatalf("recipeStore.Current: %v", err)
	}
	if newCurrent.Id == seeded.Id {
		t.Fatalf("Current().Id = %q, want a NEW record id (recipes version by superseding, not in-place rewrite)", newCurrent.Id)
	}
	if newCurrent.Version != 2 {
		t.Fatalf("Current().Version = %d, want 2", newCurrent.Version)
	}

	oldRec, err := knowledgeStore.Get(seeded.Id)
	if err != nil {
		t.Fatalf("Get(original): %v", err)
	}
	if oldRec.Validity.State != protocol.KnowledgeRecordValidityStateSuperseded {
		t.Fatalf("original record state = %q, want superseded", oldRec.Validity.State)
	}

	finalReview, err := reviews.GetReview(reviewID)
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if finalReview.Metadata.Status != protocol.ArtifactReviewMetadataStatusAccepted {
		t.Fatalf("review status = %q, want accepted", finalReview.Metadata.Status)
	}
}

func mustMarshalIndent(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	return string(data)
}

func mustFirstCommentID(t *testing.T, reviews *artifact.ReviewStore, reviewID string) string {
	t.Helper()
	comments, err := reviews.LatestComments(reviewID)
	if err != nil {
		t.Fatalf("LatestComments: %v", err)
	}
	if len(comments) == 0 {
		t.Fatal("no comments found")
	}
	return comments[0].Id
}
