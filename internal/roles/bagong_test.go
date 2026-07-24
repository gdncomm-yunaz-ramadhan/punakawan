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
		Findings: []protocol.KnowledgeRecordBagongReviewFindingsElem{{
			Severity:        "major",
			Location:        "internal/foo/bar.go:12",
			Why:             "the handler leaks a goroutine on every request",
			FailureScenario: "under sustained load the process OOMs and is OOM-killed",
			Correction:      "defer cancel() on the derived context",
		}},
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

// completeFinding returns a fully-populated structured finding that passes
// every per-finding rubric check, so each subtest can knock out one field.
func completeFinding() protocol.KnowledgeRecordBagongReviewBlockingFindingsElem {
	return protocol.KnowledgeRecordBagongReviewBlockingFindingsElem{
		Severity:        "blocker",
		Location:        "internal/checkout/total.go:42",
		Why:             "checkout total is off by one cent on discount codes",
		FailureScenario: "a $10 order with a 10% code charges $8.99 instead of $9.00",
		Correction:      "round after summing line items",
	}
}

func TestSubmitBagongReviewRejectsFindingMissingRequiredField(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*protocol.KnowledgeRecordBagongReviewBlockingFindingsElem)
		wantMsg string
	}{
		{"severity", func(f *protocol.KnowledgeRecordBagongReviewBlockingFindingsElem) { f.Severity = "" }, "severity"},
		{"location", func(f *protocol.KnowledgeRecordBagongReviewBlockingFindingsElem) { f.Location = "" }, "location"},
		{"why", func(f *protocol.KnowledgeRecordBagongReviewBlockingFindingsElem) { f.Why = "" }, "why"},
		{"failure_scenario", func(f *protocol.KnowledgeRecordBagongReviewBlockingFindingsElem) { f.FailureScenario = "" }, "failure_scenario"},
		{"correction", func(f *protocol.KnowledgeRecordBagongReviewBlockingFindingsElem) { f.Correction = "" }, "correction"},
		{"invalid-severity", func(f *protocol.KnowledgeRecordBagongReviewBlockingFindingsElem) { f.Severity = "nit" }, "invalid severity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			review := conformantReview()
			review.HonestSummary = strPtr("summary")
			f := completeFinding()
			tc.mutate(&f)
			review.BlockingFindings = []protocol.KnowledgeRecordBagongReviewBlockingFindingsElem{f}
			_, err := SubmitBagongReview(nil, "pkw:bagong/ws/run-1", "review", review)
			if err == nil {
				t.Fatalf("expected rejection when a blocking finding lacks %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantMsg)
			}
		})
	}
}

func TestSubmitBagongReviewRejectsNonBlockingFindingMissingField(t *testing.T) {
	review := conformantReview()
	review.HonestSummary = strPtr("summary")
	review.Findings = []protocol.KnowledgeRecordBagongReviewFindingsElem{{
		Severity: "minor",
		Location: "internal/foo/bar.go:5",
		// why, failure_scenario, correction intentionally omitted
	}}
	_, err := SubmitBagongReview(nil, "pkw:bagong/ws/run-1", "review", review)
	if err == nil {
		t.Fatal("expected rejection when a non-blocking finding is missing fields")
	}
	if !strings.Contains(err.Error(), "findings[0]") || !strings.Contains(err.Error(), "why") {
		t.Errorf("error = %q, want it to name findings[0] and the missing field", err)
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
		Findings: []protocol.KnowledgeRecordBagongReviewFindingsElem{{
			Severity:        "major",
			Location:        "internal/refund/handler.go:88",
			Why:             "the refund handler is not idempotent",
			FailureScenario: "a retried webhook double-refunds the customer",
			Correction:      "guard on an idempotency key",
		}},
		Uncertainties: []string{"could not verify webhook retry behavior without an integration test"},
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
