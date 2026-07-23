package validation

import (
	"strconv"
	"strings"
)

// §11's "consistency checks" (goals/non-goals don't contradict, delivery
// phases preserve dependencies, acceptance criteria cover new behavior,
// removed requirements listed explicitly, new runtime deps declared, security
// restrictions not weakened without a reviewed comment) each require actual
// reading comprehension of prose - there is no deterministic heuristic for "do
// these two paragraphs contradict" (punokawan-apy.6.1). This package therefore
// does NOT fake a text heuristic. Instead the revising agent self-attests each
// item, and ValidateConsistency enforces that the self-report is COMPLETE and
// candid: every check addressed exactly once, justified when claimed satisfied,
// and any agent-declared violation surfaced as blocking. It is a
// completeness/candor gate over the agent's own judgment, not a prose analyzer.
type ConsistencyCheck string

const (
	ConsistencyGoalsNonGoals       ConsistencyCheck = "goals_non_goals_no_contradiction"
	ConsistencyPhaseDependencies   ConsistencyCheck = "delivery_phases_preserve_dependencies"
	ConsistencyAcceptanceCoverage  ConsistencyCheck = "acceptance_criteria_cover_new_behavior"
	ConsistencyRemovedRequirements ConsistencyCheck = "removed_requirements_listed_explicitly"
	ConsistencyRuntimeDeps         ConsistencyCheck = "new_runtime_dependencies_declared"
	ConsistencySecurity            ConsistencyCheck = "security_restrictions_not_weakened_unreviewed"
)

// RequiredConsistencyChecks is the full §11 consistency checklist the revising
// agent must address before a proposal is accepted.
var RequiredConsistencyChecks = []ConsistencyCheck{
	ConsistencyGoalsNonGoals,
	ConsistencyPhaseDependencies,
	ConsistencyAcceptanceCoverage,
	ConsistencyRemovedRequirements,
	ConsistencyRuntimeDeps,
	ConsistencySecurity,
}

// ConsistencyStatus is the revising agent's verdict on one check.
type ConsistencyStatus string

const (
	ConsistencySatisfied     ConsistencyStatus = "satisfied"
	ConsistencyNotApplicable ConsistencyStatus = "not_applicable"
	ConsistencyViolation     ConsistencyStatus = "violation"
)

// ConsistencyAttestation is the revising agent's self-report for one check.
type ConsistencyAttestation struct {
	Check  ConsistencyCheck  `json:"check"`
	Status ConsistencyStatus `json:"status"`
	Note   string            `json:"note"`
}

// ConsistencyReport is §11's "Consistency checks" result. Attested records
// whether the agent supplied any self-report at all, so a caller can decide
// whether the absence of one should block acceptance or merely be surfaced.
type ConsistencyReport struct {
	Passed   bool    `json:"passed"`
	Attested bool    `json:"attested"`
	Issues   []Issue `json:"issues"`
}

// ValidateConsistency enforces the self-report is complete and candid without
// judging any prose: every required check must be attested exactly once; a
// satisfied/not_applicable attestation must carry a justifying note; an
// agent-declared violation is a blocking issue; unknown checks and invalid
// statuses are rejected. With no attestations at all it returns a non-passing,
// not-Attested report carrying a single informational issue.
func ValidateConsistency(attestations []ConsistencyAttestation) ConsistencyReport {
	if len(attestations) == 0 {
		return ConsistencyReport{
			Passed:   false,
			Attested: false,
			Issues: []Issue{{
				Check:   "consistency_attestation",
				Message: "§11 consistency checks were not self-attested; the revising agent should report each item",
			}},
		}
	}

	valid := make(map[ConsistencyCheck]bool, len(RequiredConsistencyChecks))
	for _, c := range RequiredConsistencyChecks {
		valid[c] = true
	}

	var issues []Issue
	seen := make(map[ConsistencyCheck]int, len(RequiredConsistencyChecks))
	for _, a := range attestations {
		if !valid[a.Check] {
			issues = append(issues, Issue{Check: string(a.Check), Message: "unknown consistency check"})
			continue
		}
		seen[a.Check]++
		switch a.Status {
		case ConsistencySatisfied, ConsistencyNotApplicable:
			if strings.TrimSpace(a.Note) == "" {
				issues = append(issues, Issue{Check: string(a.Check), Message: "a " + string(a.Status) + " attestation must include a justifying note"})
			}
		case ConsistencyViolation:
			msg := "the revising agent declared a consistency violation"
			if n := strings.TrimSpace(a.Note); n != "" {
				msg += ": " + n
			}
			issues = append(issues, Issue{Check: string(a.Check), Message: msg})
		default:
			issues = append(issues, Issue{Check: string(a.Check), Message: "invalid status " + strconv.Quote(string(a.Status))})
		}
	}

	for _, c := range RequiredConsistencyChecks {
		switch seen[c] {
		case 0:
			issues = append(issues, Issue{Check: string(c), Message: "required consistency check was not attested"})
		case 1:
			// addressed exactly once
		default:
			issues = append(issues, Issue{Check: string(c), Message: "consistency check attested more than once"})
		}
	}

	return ConsistencyReport{Passed: len(issues) == 0, Attested: true, Issues: issues}
}
