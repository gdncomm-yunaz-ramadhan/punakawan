package roles

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestSubmitGarengReviewRequiresVerdict(t *testing.T) {
	if _, err := SubmitGarengReview(nil, "pkw:gareng/ws/run-1", "review", protocol.KnowledgeRecordGarengReview{}); err == nil {
		t.Fatal("expected error for missing verdict")
	}
}

func TestSubmitGarengReviewBlockingFindingRequiresEvidence(t *testing.T) {
	store := newTestStore(t)

	// A blocking finding with no backing evidence is rejected: a blocker must
	// carry evidence or a concrete failure scenario, else it is a mere risk.
	unbacked := protocol.KnowledgeRecordGarengReview{
		Verdict:          strPtr("changes_required"),
		BlockingFindings: []string{"the migration is not reversible"},
	}
	if _, err := SubmitGarengReview(store, "pkw:gareng/ws/run-1", "review", unbacked); err == nil {
		t.Fatal("expected error: a blocking finding without required_evidence must be rejected")
	}

	// The same finding with backing evidence is accepted.
	backed := unbacked
	backed.RequiredEvidence = []string{"migration rollback test showing data loss"}
	if _, err := SubmitGarengReview(store, "pkw:gareng/ws/run-1", "review", backed); err != nil {
		t.Fatalf("blocking finding with evidence should be accepted: %v", err)
	}

	// A review with no blocking findings needs no required_evidence.
	nonBlocking := protocol.KnowledgeRecordGarengReview{
		Verdict:             strPtr("ok"),
		NonBlockingFindings: []string{"consider adding a metric"},
	}
	if _, err := SubmitGarengReview(store, "pkw:gareng/ws/run-2", "review", nonBlocking); err != nil {
		t.Fatalf("non-blocking review should not require evidence: %v", err)
	}
}

func TestSubmitGarengReviewPersists(t *testing.T) {
	store := newTestStore(t)

	review := protocol.KnowledgeRecordGarengReview{
		Verdict:             strPtr("clarification_required"),
		BlockingFindings:    []string{"no rollback plan"},
		RequiredEvidence:    []string{"load test results"},
		RecommendedDefaults: []string{"default to soft delete"},
	}

	rec, err := SubmitGarengReview(store, "pkw:gareng/ws/run-1", "Gareng review of refund API", review)
	if err != nil {
		t.Fatalf("SubmitGarengReview: %v", err)
	}
	if rec.Type != protocol.KnowledgeRecordTypeGarengReview {
		t.Fatalf("Type = %q, want gareng-review", rec.Type)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.GarengReview == nil || *got.GarengReview.Verdict != "clarification_required" {
		t.Fatalf("GarengReview = %+v, want verdict clarification_required", got.GarengReview)
	}
	if got.Validity.State != protocol.KnowledgeRecordValidityStateInferred {
		t.Fatalf("Validity.State = %q, want inferred", got.Validity.State)
	}
}
