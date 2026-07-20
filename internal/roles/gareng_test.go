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
