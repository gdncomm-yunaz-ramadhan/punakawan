package recipe

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/pkg/protocol"
)

type fakeSearch struct {
	issues []ResultRow
	err    error
	// calls counts every Search invocation, for tests that assert a code
	// path did (or deliberately did not) trigger an extra provider round
	// trip - e.g. a matching instance fingerprint must not force a
	// redundant revalidation dry run.
	calls int
}

func (f *fakeSearch) Search(ctx context.Context, jql, orderBy string, fields []string, maxResults int) ([]ResultRow, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.issues, nil
}

func validationRecipe() *protocol.KnowledgeRecordRetrievalRecipe {
	anyOp := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElemOperatorPhraseContains
	return baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			allEquals("project", literalValue("TRF")),
			{
				Any: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElem{
					{Field: strField("component"), Operator: opPtr(protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElemOperatorEquals), Value: literalValue("AFFILIATE-PLATFORM")},
					{Field: strField("summary"), Operator: &anyOp, Value: literalValue("AFFILIATE PLATFORM")},
				},
			},
		},
	})
}

func opPtr(o protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElemOperator) *protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElemOperator {
	return &o
}

func TestValidateNoSearchClientFails(t *testing.T) {
	v := &Validator{Compiler: NewCompiler(nil)}
	report, err := v.Validate(context.Background(), validationRecipe(), nil, nil, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.Status != protocol.KnowledgeRecordRetrievalRecipeValidationStatusFailed {
		t.Fatalf("Status = %q, want failed", report.Status)
	}
	if !strings.Contains(report.FailureReason, "no Jira search client") {
		t.Fatalf("FailureReason = %q, want a mention of the missing search client", report.FailureReason)
	}
}

func TestValidatePassesAndAttributesMatchReasons(t *testing.T) {
	search := &fakeSearch{issues: []ResultRow{
		{Key: "TRF-1842", Summary: "Affiliate payout retry", Fields: map[string]interface{}{"component": "AFFILIATE-PLATFORM"}},
		{Key: "TRF-1851", Summary: "AFFILIATE PLATFORM dashboard audit", Fields: map[string]interface{}{"component": "WEB"}},
	}}
	v := &Validator{Compiler: NewCompiler(nil), Search: search}

	report, err := v.Validate(context.Background(), validationRecipe(), nil, nil, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.Status != protocol.KnowledgeRecordRetrievalRecipeValidationStatusPassed {
		t.Fatalf("Status = %q, want passed (FailureReason=%q)", report.Status, report.FailureReason)
	}
	if report.ResultCount != 2 {
		t.Fatalf("ResultCount = %d, want 2", report.ResultCount)
	}
	if len(report.Samples) != 2 {
		t.Fatalf("Samples = %v, want 2 entries", report.Samples)
	}
	if len(report.Samples[0].MatchReasons) != 1 || report.Samples[0].MatchReasons[0] != "component" {
		t.Fatalf("Samples[0].MatchReasons = %v, want [component]", report.Samples[0].MatchReasons)
	}
	if len(report.Samples[1].MatchReasons) != 1 || report.Samples[1].MatchReasons[0] != "summary" {
		t.Fatalf("Samples[1].MatchReasons = %v, want [summary]", report.Samples[1].MatchReasons)
	}
}

func TestValidateFailsBelowMinResults(t *testing.T) {
	search := &fakeSearch{issues: nil}
	v := &Validator{Compiler: NewCompiler(nil), Search: search}

	report, err := v.Validate(context.Background(), validationRecipe(), nil, nil, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.Status != protocol.KnowledgeRecordRetrievalRecipeValidationStatusFailed {
		t.Fatalf("Status = %q, want failed for zero results", report.Status)
	}
}

func TestValidateFailsWhenExcludedResultStillMatches(t *testing.T) {
	search := &fakeSearch{issues: []ResultRow{{Key: "TRF-1842", Summary: "Affiliate payout retry"}}}
	v := &Validator{Compiler: NewCompiler(nil), Search: search}

	report, err := v.Validate(context.Background(), validationRecipe(), nil, []string{"TRF-1842"}, nil)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.Status != protocol.KnowledgeRecordRetrievalRecipeValidationStatusFailed {
		t.Fatalf("Status = %q, want failed for an excluded result that still matched", report.Status)
	}
}

func TestValidateFailsWhenMustIncludeMissing(t *testing.T) {
	search := &fakeSearch{issues: []ResultRow{{Key: "TRF-1842", Summary: "Affiliate payout retry"}}}
	v := &Validator{Compiler: NewCompiler(nil), Search: search}

	report, err := v.Validate(context.Background(), validationRecipe(), nil, nil, []string{"TRF-9999"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if report.Status != protocol.KnowledgeRecordRetrievalRecipeValidationStatusFailed {
		t.Fatalf("Status = %q, want failed for a missing expected result", report.Status)
	}
}

func TestValidatePropagatesCompilerError(t *testing.T) {
	badOp := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			{Field: strField("not_a_real_field"), Operator: &badOp, Value: literalValue("x")},
		},
	})
	v := &Validator{Compiler: NewCompiler(nil), Search: &fakeSearch{}}
	if _, err := v.Validate(context.Background(), rr, nil, nil, nil); err == nil {
		t.Fatal("Validate: want a compile error for an unsupported field, got nil")
	}
}

func TestBuildValidationBlock(t *testing.T) {
	report := ValidationReport{
		Status:            protocol.KnowledgeRecordRetrievalRecipeValidationStatusPassed,
		CompiledQueryHash: "sha256:abc",
		ResultCount:       14,
		Samples:           make([]SampleResult, 14),
	}
	acceptedAt := time.Date(2026, 7, 23, 11, 41, 0, 0, time.UTC)

	block := BuildValidationBlock(report, "val-20260723-0041", "user", acceptedAt, "jira-cloud-company", []string{"ev-1"})
	if block.Status == nil || *block.Status != protocol.KnowledgeRecordRetrievalRecipeValidationStatusPassed {
		t.Fatalf("Status = %v, want passed", block.Status)
	}
	if block.SampleSize == nil || *block.SampleSize != 14 {
		t.Fatalf("SampleSize = %v, want 14", block.SampleSize)
	}
	if block.AcceptedResultCount == nil || *block.AcceptedResultCount != 14 {
		t.Fatalf("AcceptedResultCount = %v, want 14", block.AcceptedResultCount)
	}
	if block.ValidationId == nil || *block.ValidationId != "val-20260723-0041" {
		t.Fatalf("ValidationId = %v, want val-20260723-0041", block.ValidationId)
	}
}

func TestRecordValidationAndAcceptanceEvidence(t *testing.T) {
	dir := t.TempDir()
	ledger, err := evidence.OpenLedger(dir, "run-1")
	if err != nil {
		t.Fatalf("OpenLedger: %v", err)
	}

	report := ValidationReport{Status: protocol.KnowledgeRecordRetrievalRecipeValidationStatusPassed, CompiledQueryText: `project = "TRF"`, ResultCount: 14}
	if _, err := RecordValidationEvidence(ledger, "run-1", "task-1", report, time.Now()); err != nil {
		t.Fatalf("RecordValidationEvidence: %v", err)
	}
	if _, err := RecordAcceptanceEvidence(ledger, "run-1", "task-1", "user", time.Now()); err != nil {
		t.Fatalf("RecordAcceptanceEvidence: %v", err)
	}

	all, err := ledger.ForTask("task-1")
	if err != nil {
		t.Fatalf("ForTask: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ForTask returned %d records, want 2", len(all))
	}
}
