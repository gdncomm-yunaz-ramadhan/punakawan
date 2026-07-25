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
// problems is only conformant if honest_summary says so explicitly.
//
// blocking_findings and findings are now structured finding objects in
// protocol's knowledge.schema.json, so the per-finding attributes the rubric
// demands - severity (valid enum), the exact file/location, why it is a
// problem, a realistic failure scenario, and the smallest appropriate
// correction - are hard-enforced here as field-level checks: every finding in
// either section is rejected with a precise error naming the offending
// section, index, and missing field.
func validateSeniorMaintainerRubric(id string, review protocol.KnowledgeRecordBagongReview) error {
	// Section 4 - verification performed - must always be documented.
	if countNonBlank(review.RequirementCoverage) == 0 {
		return fmt.Errorf("roles: bagong review %s: senior-maintainer rubric requires a 'verification performed' section - populate requirement_coverage with what you actually verified against the requirement, diff, and test evidence (a review that verifies nothing is not a senior-maintainer review)", id)
	}
	// Section 3 - questions/assumptions and remaining unverified risks.
	if countNonBlank(review.Uncertainties) == 0 {
		return fmt.Errorf("roles: bagong review %s: senior-maintainer rubric requires a 'questions/assumptions' section - populate uncertainties with open questions, assumptions, and any remaining risks you could not verify (the rubric requires you to identify remaining unverified risks even when no problems are found)", id)
	}
	// Sections 1 and 2 - every finding must carry the rubric's per-finding
	// attributes. The two slices are distinct generated types with identical
	// fields, so each is projected onto the shared validateFinding checker.
	for i, f := range review.BlockingFindings {
		if err := validateFinding(id, "blocking_findings", i, string(f.Severity), f.Location, f.Why, f.FailureScenario, f.Correction); err != nil {
			return err
		}
	}
	for i, f := range review.Findings {
		if err := validateFinding(id, "findings", i, string(f.Severity), f.Location, f.Why, f.FailureScenario, f.Correction); err != nil {
			return err
		}
	}
	// "No actionable problems found" is a valid, conforming submission only
	// if honest_summary says so explicitly (rubric's last line). Remaining
	// unverified risks are already required via uncertainties above.
	if len(review.BlockingFindings) == 0 && len(review.Findings) == 0 {
		if !statesNoActionableProblems(*review.HonestSummary) {
			return fmt.Errorf("roles: bagong review %s: with no blocking or non-blocking findings, the senior-maintainer rubric requires honest_summary to state explicitly that no actionable problems were found (e.g. \"no blocking issues\", \"no actionable problems\") and rely on uncertainties for remaining unverified risks", id)
		}
	}
	return nil
}

// validFindingSeverities is the set of severities a Bagong finding may carry,
// reusing ReviewFinding's severity vocabulary (protocol
// reviewfinding.schema.json) for cross-review consistency.
var validFindingSeverities = map[string]bool{
	"blocker":    true,
	"major":      true,
	"minor":      true,
	"suggestion": true,
}

// validateFinding hard-enforces that a single structured finding carries every
// per-finding attribute the senior-maintainer rubric demands. section is the
// schema field name ("blocking_findings" or "findings") and i its index, so
// the rejection precisely names the offending finding and the missing field.
func validateFinding(id, section string, i int, severity, location, why, failureScenario, correction string) error {
	missing := func(field string) error {
		return fmt.Errorf("roles: bagong review %s: %s[%d] is missing %s - %s", id, section, i, field, findingAttributesReminder)
	}
	if strings.TrimSpace(severity) == "" {
		return missing("severity")
	}
	if !validFindingSeverities[strings.TrimSpace(severity)] {
		return fmt.Errorf("roles: bagong review %s: %s[%d] has invalid severity %q - must be one of blocker, major, minor, suggestion", id, section, i, severity)
	}
	if strings.TrimSpace(location) == "" {
		return missing("location (the exact file and line)")
	}
	if strings.TrimSpace(why) == "" {
		return missing("why (why it is a problem)")
	}
	if strings.TrimSpace(failureScenario) == "" {
		return missing("failure_scenario (a realistic failure scenario)")
	}
	if strings.TrimSpace(correction) == "" {
		return missing("correction (the smallest appropriate correction)")
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
