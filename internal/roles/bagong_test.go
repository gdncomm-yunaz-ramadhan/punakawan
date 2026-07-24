package roles

import (
	"strings"
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

// conformantReview returns a senior-maintainer-rubric-conformant review with
// findings, so individual tests can knock out one requirement at a time.
func conformantReview() protocol.KnowledgeRecordBagongReview {
	return protocol.KnowledgeRecordBagongReview{
		Verdict:             strPtr("changes_required"),
		RequirementCoverage: []string{"AC1: verified against diff.patch and tests.json"},
		Uncertainties:       []string{"could not verify concurrency behavior without a load test"},
		Findings:            []string{"major: handler at internal/foo/bar.go:12 leaks a goroutine; under sustained load the process OOMs; defer cancel() on the derived context"},
	}
}

func TestSubmitBagongReviewRubricConformantPasses(t *testing.T) {
	store := newTestStore(t)
	review := conformantReview()
	review.HonestSummary = strPtr("Confident on the happy path; one blocking leak; concurrency unverified.")
	if _, err := SubmitBagongReview(store, "pkw:bagong/ws/run-ok", "review", review); err != nil {
		t.Fatalf("conformant review should pass, got: %v", err)
	}
}

func TestSubmitBagongReviewRejectsMissingVerificationSection(t *testing.T) {
	review := conformantReview()
	review.HonestSummary = strPtr("summary")
	review.RequirementCoverage = nil // section 4 (verification performed) absent
	_, err := SubmitBagongReview(nil, "pkw:bagong/ws/run-1", "review", review)
	if err == nil {
		t.Fatal("expected rejection when requirement_coverage (verification performed) is empty")
	}
	if !strings.Contains(err.Error(), "verification performed") {
		t.Errorf("error = %q, want it to name the missing 'verification performed' section", err)
	}
}

func TestSubmitBagongReviewRejectsMissingQuestionsSection(t *testing.T) {
	review := conformantReview()
	review.HonestSummary = strPtr("summary")
	review.Uncertainties = []string{"   "} // section 3 (questions/assumptions) blank
	_, err := SubmitBagongReview(nil, "pkw:bagong/ws/run-1", "review", review)
	if err == nil {
		t.Fatal("expected rejection when uncertainties (questions/assumptions) is empty")
	}
	if !strings.Contains(err.Error(), "questions/assumptions") {
		t.Errorf("error = %q, want it to name the missing 'questions/assumptions' section", err)
	}
}

func TestSubmitBagongReviewRejectsBlankFinding(t *testing.T) {
	review := conformantReview()
	review.HonestSummary = strPtr("summary")
	review.BlockingFindings = []string{""} // a finding missing severity/location/scenario/correction
	_, err := SubmitBagongReview(nil, "pkw:bagong/ws/run-1", "review", review)
	if err == nil {
		t.Fatal("expected rejection when a blocking finding is blank")
	}
	if !strings.Contains(err.Error(), "severity") || !strings.Contains(err.Error(), "location") {
		t.Errorf("error = %q, want it to spell out the required per-finding attributes", err)
	}
}

func TestSubmitBagongReviewRejectsImplicitCleanBill(t *testing.T) {
	review := protocol.KnowledgeRecordBagongReview{
		Verdict:             strPtr("approve"),
		RequirementCoverage: []string{"AC1: verified against diff.patch"},
		Uncertainties:       []string{"concurrency unverified"},
		HonestSummary:       strPtr("Looks fine to me."), // no explicit no-problems statement
	}
	_, err := SubmitBagongReview(nil, "pkw:bagong/ws/run-1", "review", review)
	if err == nil {
		t.Fatal("expected rejection: a no-findings review must state 'no actionable problems' explicitly")
	}
	if !strings.Contains(err.Error(), "explicitly") {
		t.Errorf("error = %q, want it to require an explicit statement", err)
	}
}

func TestSubmitBagongReviewAcceptsExplicitCleanBill(t *testing.T) {
	store := newTestStore(t)
	review := protocol.KnowledgeRecordBagongReview{
		Verdict:             strPtr("approve"),
		RequirementCoverage: []string{"AC1: verified against diff.patch and tests.json"},
		Uncertainties:       []string{"concurrency behavior unverified (no load test)"},
		HonestSummary:       strPtr("No actionable problems found; confident on correctness."),
	}
	if _, err := SubmitBagongReview(store, "pkw:bagong/ws/run-clean", "review", review); err != nil {
		t.Fatalf("explicit clean review should pass, got: %v", err)
	}
}

func TestSubmitBagongReviewPersists(t *testing.T) {
	store := newTestStore(t)

	review := protocol.KnowledgeRecordBagongReview{
		Verdict:             strPtr("changes_required"),
		RequirementCoverage: []string{"AC1 refund happy path: verified against diff.patch and tests.json"},
		TestGaps:            []string{"no test for duplicate refund requests"},
		Findings:            []string{"major: refund handler at internal/refund/handler.go:88 is not idempotent; a retried webhook double-refunds; guard on an idempotency key"},
		Uncertainties:       []string{"could not verify webhook retry behavior without an integration test"},
		HonestSummary:       strPtr("Implementation covers the happy path but idempotency is untested."),
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
