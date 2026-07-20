package roles

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestSubmitBagongReviewRequiresVerdictAndHonestSummary(t *testing.T) {
	if _, err := SubmitBagongReview(nil, "pkw:bagong/ws/run-1", "review", protocol.KnowledgeRecordBagongReview{}); err == nil {
		t.Fatal("expected error for missing verdict")
	}
	if _, err := SubmitBagongReview(nil, "pkw:bagong/ws/run-1", "review", protocol.KnowledgeRecordBagongReview{
		Verdict: strPtr("changes_required"),
	}); err == nil {
		t.Fatal("expected error for missing honest_summary")
	}
}

func TestSubmitBagongReviewPersists(t *testing.T) {
	store := newTestStore(t)

	review := protocol.KnowledgeRecordBagongReview{
		Verdict:       strPtr("changes_required"),
		TestGaps:      []string{"no test for duplicate refund requests"},
		HonestSummary: strPtr("Implementation covers the happy path but idempotency is untested."),
	}

	rec, err := SubmitBagongReview(store, "pkw:bagong/ws/run-1", "Bagong review of refund API", review)
	if err != nil {
		t.Fatalf("SubmitBagongReview: %v", err)
	}
	if rec.Type != protocol.KnowledgeRecordTypeBagongReview {
		t.Fatalf("Type = %q, want bagong-review", rec.Type)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BagongReview == nil || *got.BagongReview.Verdict != "changes_required" {
		t.Fatalf("BagongReview = %+v, want verdict changes_required", got.BagongReview)
	}
}
