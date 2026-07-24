package roles

import (
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubmitBagongReview validates and persists Bagong's independent final
// review (§8.4) as a bagong-review knowledge record. Bagong's
// responsibilities explicitly include an "honest confidence statement", so
// honest_summary is required in addition to verdict.
//
// Every Bagong review is a code/diff review (§8.4 lists "Diff review" and
// "API compatibility review" among Bagong's core responsibilities and
// ADR-0015 has Bagong review raw diffs/evidence independently), so the
// senior-maintainer review rubric below is enforced as a hard constraint on
// every submission rather than being gated on a per-call "is this a code
// review" flag - Bagong is Punakawan's independent code reviewer, so there
// is no non-code review path for this role to accidentally over-constrain.
func SubmitBagongReview(store *knowledge.Store, id, title string, review protocol.KnowledgeRecordBagongReview) (protocol.KnowledgeRecord, error) {
	if review.Verdict == nil || *review.Verdict == "" {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: bagong review %s: verdict is required", id)
	}
	if review.HonestSummary == nil || *review.HonestSummary == "" {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: bagong review %s: honest_summary is required (§8.4)", id)
	}
	if err := validateSeniorMaintainerRubric(id, review); err != nil {
		return protocol.KnowledgeRecord{}, err
	}

	rec := newSubmissionRecord(id, title, protocol.KnowledgeRecordTypeBagongReview)
	rec.BagongReview = &review
	if err := store.Put(rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: submit bagong review %s: %w", id, err)
	}
	return rec, nil
}

// findingAttributesReminder is the per-finding contract every reported
// finding must satisfy under the senior-maintainer rubric. It is reused
// verbatim across the rejection messages so the caller always sees the same
// concrete checklist.
const findingAttributesReminder = "every finding must carry severity, the exact file and location, why it is a problem, a realistic failure scenario, and the smallest appropriate correction"

// validateSeniorMaintainerRubric enforces the mandatory senior-maintainer
// review rubric (embedded verbatim in prompts/bagong/prompt.md) as a hard
// constraint on a code/PR/diff review submission. A conforming review must
// separate its output into the rubric's four mandatory sections, which map
// onto the bagong_review schema fields as follows:
//
//  1. blocking findings          -> blocking_findings
//  2. non-blocking improvements  -> findings
//  3. questions or assumptions   -> uncertainties
//  4. verification performed     -> requirement_coverage
//
// Sections 3 and 4 must always be populated: even a clean review must state
// what was actually verified (section 4) and list any remaining risks it
// could not verify (section 3, per the rubric's final line). Sections 1 and
// 2 may legitimately be empty, but a review that reports no actionable
// problems is only conformant if honest_summary says so explicitly. Because
// blocking_findings/findings are free-text strings in protocol's
// knowledge.schema.json (not structured finding objects), the per-finding
// attributes - severity, file/location, why, failure scenario, smallest
// correction - are directed by the embedded rubric instruction rather than
// field-validated here; this gate additionally rejects blank finding entries
// so an "empty" finding cannot slip through. Turning the per-finding
// attributes into hard field-level checks would require a protocol schema
// change (structured findings), which is flagged rather than hand-edited.
func validateSeniorMaintainerRubric(id string, review protocol.KnowledgeRecordBagongReview) error {
	// Section 4 - verification performed - must always be documented.
	if countNonBlank(review.RequirementCoverage) == 0 {
		return fmt.Errorf("roles: bagong review %s: senior-maintainer rubric requires a 'verification performed' section - populate requirement_coverage with what you actually verified against the requirement, diff, and test evidence (a review that verifies nothing is not a senior-maintainer review)", id)
	}
	// Section 3 - questions/assumptions and remaining unverified risks.
	if countNonBlank(review.Uncertainties) == 0 {
		return fmt.Errorf("roles: bagong review %s: senior-maintainer rubric requires a 'questions/assumptions' section - populate uncertainties with open questions, assumptions, and any remaining risks you could not verify (the rubric requires you to identify remaining unverified risks even when no problems are found)", id)
	}
	// Sections 1 and 2 - findings must be substantive when present.
	for i, f := range review.BlockingFindings {
		if strings.TrimSpace(f) == "" {
			return fmt.Errorf("roles: bagong review %s: blocking_findings[%d] is blank - %s", id, i, findingAttributesReminder)
		}
	}
	for i, f := range review.Findings {
		if strings.TrimSpace(f) == "" {
			return fmt.Errorf("roles: bagong review %s: findings[%d] is blank - %s", id, i, findingAttributesReminder)
		}
	}
	// "No actionable problems found" is a valid, conforming submission only
	// if honest_summary says so explicitly (rubric's last line). Remaining
	// unverified risks are already required via uncertainties above.
	if countNonBlank(review.BlockingFindings) == 0 && countNonBlank(review.Findings) == 0 {
		if !statesNoActionableProblems(*review.HonestSummary) {
			return fmt.Errorf("roles: bagong review %s: with no blocking or non-blocking findings, the senior-maintainer rubric requires honest_summary to state explicitly that no actionable problems were found (e.g. \"no blocking issues\", \"no actionable problems\") and rely on uncertainties for remaining unverified risks", id)
		}
	}
	return nil
}

// countNonBlank returns how many entries of s are non-empty after trimming.
func countNonBlank(s []string) int {
	n := 0
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

// statesNoActionableProblems reports whether an honest_summary explicitly
// declares a clean bill of health, as the rubric requires when no findings
// are reported. The match is deliberately lenient - any of a small set of
// unambiguous phrasings satisfies it - so that a genuinely explicit summary
// is never rejected, while a summary that simply omits the statement is.
func statesNoActionableProblems(summary string) bool {
	s := strings.ToLower(summary)
	for _, phrase := range []string{
		"no blocking",
		"no actionable",
		"no problem",
		"no issue",
		"no finding",
		"no concern",
		"no critical",
		"nothing actionable",
		"nothing blocking",
	} {
		if strings.Contains(s, phrase) {
			return true
		}
	}
	return false
}
