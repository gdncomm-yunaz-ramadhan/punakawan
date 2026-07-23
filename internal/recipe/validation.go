package recipe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// JiraIssue is the minimal shape a provider dry run returns: enough to
// count, sample, and infer match reasons, not a full issue mirror.
type JiraIssue struct {
	Key     string
	Summary string
	// Fields holds raw field values keyed by selector field name (e.g.
	// "component", "status"), used only for matchReasons' best-effort
	// inference - a field this recipe's output didn't request may be
	// absent, in which case that clause simply contributes no reason.
	Fields map[string]interface{}
}

// JiraSearchClient runs a compiled query against the real provider. No
// concrete implementation exists yet: packages/adapter-atlassian's
// searchJira operation exists, but nothing on the Go side calls it -
// wiring this to call_adapter_operation is §15's RecipeExecutor, a later
// phase's job, same honest gap as Compiler's JiraAgileClient. A nil
// client makes Validate fail step 6 explicitly rather than silently
// reporting zero results as a pass.
type JiraSearchClient interface {
	Search(ctx context.Context, jql, orderBy string, fields []string, maxResults int) ([]JiraIssue, error)
}

// SampleResult is one representative row, per §9's presentation table.
type SampleResult struct {
	Key          string
	Summary      string
	MatchReasons []string
}

// ValidationReport is §9's validation report. Status/CompiledQueryHash
// mirror protocol.KnowledgeRecordRetrievalRecipeValidation's fields
// directly (see BuildValidationBlock) so there is no second translation
// step between what Validate produces and what gets embedded in a
// recipe's validation block.
type ValidationReport struct {
	Status            protocol.KnowledgeRecordRetrievalRecipeValidationStatus
	JQL               string
	CompiledQueryHash string
	ResultCount       int
	Samples           []SampleResult
	// Warnings carries the compiler's own approximation warnings (e.g. a
	// futureSprints() fallback) plus any validation-specific ones (a
	// broader-than-expected result count).
	Warnings []string
	// FailureReason is set only when Status is failed.
	FailureReason string
}

const maxSampleSize = 20

// Validator runs §9's validation pipeline. Steps 1-5 (schema,
// capability, field/operator, and dynamic-resolver validation, plus
// query compilation) happen inside Compiler.Compile, which Validate
// calls first - a malformed recipe or an out-of-policy field never
// reaches the provider. Steps 10-11 (explicit acceptance, durable
// evidence) are the caller's job via DiscoverySession.Accept and
// RecordAcceptanceEvidence/RecordValidationEvidence below, since they
// need a human decision and a run/task identity this package doesn't
// own.
type Validator struct {
	Compiler *Compiler
	Search   JiraSearchClient
	// MinResults bounds step 7's sanity check from below; 0 defaults to
	// 1, since a query compiling to zero live results is usually a
	// mistake, not a valid recipe. MaxResults bounds it from above as a
	// soft warning (not a failure) that the selector may be too broad;
	// 0 means unbounded.
	MinResults int
	MaxResults int
}

// Validate runs the pipeline against rr, using bindings for its declared
// inputs and exclusions/mustInclude for step 9's negative-example check
// (typically DiscoverySession.Exclusions/MustInclude).
func (v *Validator) Validate(ctx context.Context, rr *protocol.KnowledgeRecordRetrievalRecipe, bindings map[string]interface{}, exclusions, mustInclude []string) (ValidationReport, error) {
	cq, err := v.Compiler.Compile(ctx, rr, bindings)
	if err != nil {
		return ValidationReport{}, err
	}

	hash := sha256.Sum256([]byte(cq.JQL))
	report := ValidationReport{
		JQL:               cq.JQL,
		CompiledQueryHash: "sha256:" + hex.EncodeToString(hash[:]),
		Warnings:          append([]string{}, cq.Warnings...),
	}

	if v.Search == nil {
		report.Status = protocol.KnowledgeRecordRetrievalRecipeValidationStatusFailed
		report.FailureReason = "no Jira search client configured; cannot dry-run the compiled query"
		return report, nil
	}

	maxResults := v.MaxResults
	if maxResults <= 0 || maxResults > maxSampleSize {
		maxResults = maxSampleSize
	}
	issues, err := v.Search.Search(ctx, cq.JQL, cq.OrderBy, cq.Fields, maxResults)
	if err != nil {
		report.Status = protocol.KnowledgeRecordRetrievalRecipeValidationStatusFailed
		report.FailureReason = fmt.Sprintf("provider dry run failed: %v", err)
		return report, nil
	}
	report.ResultCount = len(issues)

	minResults := v.MinResults
	if minResults <= 0 {
		minResults = 1
	}
	if report.ResultCount < minResults {
		report.Status = protocol.KnowledgeRecordRetrievalRecipeValidationStatusFailed
		report.FailureReason = fmt.Sprintf("query returned %d results, want at least %d", report.ResultCount, minResults)
		return report, nil
	}
	if v.MaxResults > 0 && report.ResultCount > v.MaxResults {
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"query returned %d results, more than the expected %d - the selector may be too broad", report.ResultCount, v.MaxResults))
	}

	for _, issue := range issues {
		report.Samples = append(report.Samples, SampleResult{
			Key:          issue.Key,
			Summary:      issue.Summary,
			MatchReasons: matchReasons(rr, issue),
		})
	}

	resultKeys := make(map[string]bool, len(issues))
	for _, issue := range issues {
		resultKeys[issue.Key] = true
	}
	for _, ex := range exclusions {
		if resultKeys[ex] {
			report.Status = protocol.KnowledgeRecordRetrievalRecipeValidationStatusFailed
			report.FailureReason = fmt.Sprintf("excluded result %q still matched the compiled query", ex)
			return report, nil
		}
	}
	for _, want := range mustInclude {
		if !resultKeys[want] {
			report.Status = protocol.KnowledgeRecordRetrievalRecipeValidationStatusFailed
			report.FailureReason = fmt.Sprintf("expected result %q did not match the compiled query", want)
			return report, nil
		}
	}

	report.Status = protocol.KnowledgeRecordRetrievalRecipeValidationStatusPassed
	return report, nil
}

// matchReasons attributes each of rr's literal-valued leaf clauses that
// plausibly matched issue, per §9's "10 matched by component, 3 matched
// by title" presentation. Resolver-valued clauses (a dynamic sprint or
// board) are not attributed a reason: the plan's own example table only
// lists component/title reasons, never the sprint clause every result
// necessarily shares, so skipping dynamic clauses here is consistent
// with the example rather than a shortcut around it.
func matchReasons(rr *protocol.KnowledgeRecordRetrievalRecipe, issue JiraIssue) []string {
	var reasons []string
	check := func(field, operator string, value interface{}) {
		m, ok := value.(map[string]interface{})
		if !ok {
			return
		}
		lit, ok := m["literal"]
		if !ok {
			return
		}
		litStr := strings.ToLower(fmt.Sprintf("%v", lit))

		actual, _ := issue.Fields[field].(string)
		if field == "summary" && actual == "" {
			actual = issue.Summary
		}
		actual = strings.ToLower(actual)

		switch operator {
		case "equals":
			if actual == litStr {
				reasons = append(reasons, field)
			}
		case "contains", "phrase_contains":
			if actual != "" && strings.Contains(actual, litStr) {
				reasons = append(reasons, field)
			}
		}
	}

	for _, e := range rr.Selector.All {
		walkAllElemLeaves(e, check)
	}
	for _, e := range rr.Selector.Any {
		walkAnyElemLeaves(e, check)
	}
	return reasons
}

func walkAllElemLeaves(e protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem, check func(field, operator string, value interface{})) {
	if e.Field != nil {
		check(*e.Field, opString(e.Operator), e.Value)
		return
	}
	for _, leaf := range e.All {
		if leaf.Field != nil {
			check(*leaf.Field, opString(leaf.Operator), leaf.Value)
		}
	}
	for _, leaf := range e.Any {
		if leaf.Field != nil {
			check(*leaf.Field, opString(leaf.Operator), leaf.Value)
		}
	}
}

func walkAnyElemLeaves(e protocol.KnowledgeRecordRetrievalRecipeSelectorAnyElem, check func(field, operator string, value interface{})) {
	if e.Field != nil {
		check(*e.Field, opString(e.Operator), e.Value)
		return
	}
	for _, leaf := range e.All {
		if leaf.Field != nil {
			check(*leaf.Field, opString(leaf.Operator), leaf.Value)
		}
	}
	for _, leaf := range e.Any {
		if leaf.Field != nil {
			check(*leaf.Field, opString(leaf.Operator), leaf.Value)
		}
	}
}

// BuildValidationBlock turns report into the recipe schema's validation
// block (§4), ready to embed in a KnowledgeRecordRetrievalRecipe before
// Repository.CreateVersion persists it. acceptedBy/acceptedAt/
// validationID/providerFingerprint/evidenceIDs are the caller's job to
// supply - this package has no identity, clock, or evidence-store
// dependency of its own for step 10-11's human-acceptance/evidence data.
func BuildValidationBlock(report ValidationReport, validationID, acceptedBy string, acceptedAt time.Time, providerFingerprint string, evidenceIDs []string) protocol.KnowledgeRecordRetrievalRecipeValidation {
	status := report.Status
	sampleSize := len(report.Samples)
	accepted := report.ResultCount
	hash := report.CompiledQueryHash
	return protocol.KnowledgeRecordRetrievalRecipeValidation{
		Status:                      &status,
		ValidationId:                &validationID,
		ProviderInstanceFingerprint: &providerFingerprint,
		CompiledQueryHash:           &hash,
		SampleSize:                  &sampleSize,
		AcceptedResultCount:         &accepted,
		AcceptedBy:                  &acceptedBy,
		AcceptedAt:                  &acceptedAt,
		EvidenceIds:                 evidenceIDs,
	}
}

// RecordValidationEvidence appends an evidence record summarizing a
// dry-run/validation attempt (the plan's ev-jql-compile-*/ev-jql-sample-*
// examples), so it becomes referenceable from a recipe's
// validation.evidence_ids.
func RecordValidationEvidence(l *evidence.Ledger, runID, taskID string, report ValidationReport, now time.Time) (protocol.EvidenceRecord, error) {
	summary := fmt.Sprintf("compiled JQL: %s | result_count=%d sample_size=%d status=%s",
		report.JQL, report.ResultCount, len(report.Samples), report.Status)
	rec := protocol.EvidenceRecord{
		Id:        fmt.Sprintf("ev-%s-%s-jql-validate-%d", runID, taskID, now.UnixNano()),
		RunId:     runID,
		TaskId:    &taskID,
		Type:      protocol.EvidenceRecordTypeExternalResponse,
		Summary:   &summary,
		CreatedAt: now,
	}
	if err := l.Append(rec); err != nil {
		return protocol.EvidenceRecord{}, err
	}
	return rec, nil
}

// RecordAcceptanceEvidence appends an evidence record for step 10's
// explicit user acceptance (the plan's ev-user-acceptance-* example).
func RecordAcceptanceEvidence(l *evidence.Ledger, runID, taskID, acceptedBy string, now time.Time) (protocol.EvidenceRecord, error) {
	summary := fmt.Sprintf("recipe accepted by %s at %s", acceptedBy, now.Format(time.RFC3339))
	rec := protocol.EvidenceRecord{
		Id:        fmt.Sprintf("ev-%s-%s-user-acceptance-%d", runID, taskID, now.UnixNano()),
		RunId:     runID,
		TaskId:    &taskID,
		Type:      protocol.EvidenceRecordTypeUserAnswer,
		Summary:   &summary,
		CreatedAt: now,
	}
	if err := l.Append(rec); err != nil {
		return protocol.EvidenceRecord{}, err
	}
	return rec, nil
}
