package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/validation"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// consistencyAttestations builds a full §11 self-report, optionally turning one
// check into a declared violation.
func consistencyAttestations(violateIndex int) []validation.ConsistencyAttestation {
	out := make([]validation.ConsistencyAttestation, 0, len(validation.RequiredConsistencyChecks))
	for i, c := range validation.RequiredConsistencyChecks {
		st := validation.ConsistencySatisfied
		note := "checked"
		if i == violateIndex {
			st = validation.ConsistencyViolation
			note = "goals and non-goals conflict"
		}
		out = append(out, validation.ConsistencyAttestation{Check: c, Status: st, Note: note})
	}
	return out
}

func addressedResolution() []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem {
	explanation := "Added an optional authenticated LAN mode section."
	return []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem{
		{CommentId: "c-1", Status: protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed, Explanation: &explanation, ChangedBlockIds: []string{"panel.security"}},
	}
}

// TestCreateProposalHandlerConsistencyViolationFailsValidation covers the
// apy.6.1 gate: an attested violation flips the stored validation status to
// failed even when structural + compliance pass.
func TestCreateProposalHandlerConsistencyViolationFailsValidation(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	body, _ := json.Marshal(createProposalRequest{
		Content:                 proposalRevisedPlanContent,
		CommentResolutions:      addressedResolution(),
		ConsistencyAttestations: consistencyAttestations(0),
	})
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
		t.Fatalf("structural/compliance should pass so the test isolates consistency: %+v", resp)
	}
	if !resp.Consistency.Attested || resp.Consistency.Passed {
		t.Fatalf("expected an attested, non-passing consistency report, got %+v", resp.Consistency)
	}
	if resp.Proposal.Results == nil || resp.Proposal.Results.ValidationStatus == nil ||
		*resp.Proposal.Results.ValidationStatus != protocol.ArtifactRevisionProposalResultsValidationStatusFailed {
		t.Fatalf("validation status should be failed when a violation is attested, got %+v", resp.Proposal.Results)
	}
}

// TestCreateProposalHandlerNoAttestationDoesNotBlock covers the other half of
// the gate: a missing self-report is surfaced but does not fail an otherwise
// passing proposal (backward compatibility for existing callers).
func TestCreateProposalHandlerNoAttestationDoesNotBlock(t *testing.T) {
	reviewID, plans, reviews := seedQueuedReviewWithComment(t)
	body, _ := json.Marshal(createProposalRequest{
		Content:            proposalRevisedPlanContent,
		CommentResolutions: addressedResolution(),
	})
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
	if resp.Consistency.Attested {
		t.Fatalf("expected not-attested consistency report, got %+v", resp.Consistency)
	}
	if resp.Proposal.Results == nil || resp.Proposal.Results.ValidationStatus == nil ||
		*resp.Proposal.Results.ValidationStatus != protocol.ArtifactRevisionProposalResultsValidationStatusPassed {
		t.Fatalf("a passing proposal with no attestation must still pass, got %+v", resp.Proposal.Results)
	}
}
