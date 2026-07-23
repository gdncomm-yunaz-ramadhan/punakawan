package validation

import "testing"

func allSatisfied() []ConsistencyAttestation {
	out := make([]ConsistencyAttestation, 0, len(RequiredConsistencyChecks))
	for _, c := range RequiredConsistencyChecks {
		out = append(out, ConsistencyAttestation{Check: c, Status: ConsistencySatisfied, Note: "checked"})
	}
	return out
}

func hasCheck(issues []Issue, check string) bool {
	for _, i := range issues {
		if i.Check == check {
			return true
		}
	}
	return false
}

func TestConsistencyNoAttestationsIsNotAttestedAndNotPassed(t *testing.T) {
	r := ValidateConsistency(nil)
	if r.Attested {
		t.Fatal("expected Attested=false with no attestations")
	}
	if r.Passed {
		t.Fatal("expected Passed=false with no attestations")
	}
	if len(r.Issues) != 1 || r.Issues[0].Check != "consistency_attestation" {
		t.Fatalf("expected one informational attestation issue, got %+v", r.Issues)
	}
}

func TestConsistencyAllSatisfiedPasses(t *testing.T) {
	r := ValidateConsistency(allSatisfied())
	if !r.Attested || !r.Passed {
		t.Fatalf("expected attested+passed, got %+v", r)
	}
	if len(r.Issues) != 0 {
		t.Fatalf("expected no issues, got %+v", r.Issues)
	}
}

func TestConsistencySatisfiedRequiresNote(t *testing.T) {
	att := allSatisfied()
	att[0].Note = "   "
	r := ValidateConsistency(att)
	if r.Passed {
		t.Fatal("expected failure when a satisfied attestation lacks a note")
	}
	if !hasCheck(r.Issues, string(att[0].Check)) {
		t.Fatalf("expected an issue for the note-less check, got %+v", r.Issues)
	}
}

func TestConsistencyNotApplicableWithNotePasses(t *testing.T) {
	att := allSatisfied()
	att[0].Status = ConsistencyNotApplicable
	att[0].Note = "no security surface touched"
	if r := ValidateConsistency(att); !r.Passed {
		t.Fatalf("not_applicable with a note should pass, got %+v", r.Issues)
	}
}

func TestConsistencyDeclaredViolationBlocks(t *testing.T) {
	att := allSatisfied()
	att[1].Status = ConsistencyViolation
	att[1].Note = "phase 3 now precedes its dependency in phase 4"
	r := ValidateConsistency(att)
	if r.Passed {
		t.Fatal("a declared violation must block")
	}
	if !hasCheck(r.Issues, string(att[1].Check)) {
		t.Fatalf("expected the violation surfaced, got %+v", r.Issues)
	}
}

func TestConsistencyMissingCheckIsFlagged(t *testing.T) {
	att := allSatisfied()[1:] // drop the first required check
	r := ValidateConsistency(att)
	if r.Passed {
		t.Fatal("a missing required check must fail")
	}
	if !hasCheck(r.Issues, string(RequiredConsistencyChecks[0])) {
		t.Fatalf("expected the missing check flagged, got %+v", r.Issues)
	}
}

func TestConsistencyUnknownCheckIsFlagged(t *testing.T) {
	att := append(allSatisfied(), ConsistencyAttestation{Check: "made_up_check", Status: ConsistencySatisfied, Note: "x"})
	r := ValidateConsistency(att)
	if r.Passed {
		t.Fatal("an unknown check must fail")
	}
	if !hasCheck(r.Issues, "made_up_check") {
		t.Fatalf("expected the unknown check flagged, got %+v", r.Issues)
	}
}

func TestConsistencyDuplicateCheckIsFlagged(t *testing.T) {
	att := append(allSatisfied(), ConsistencyAttestation{Check: RequiredConsistencyChecks[0], Status: ConsistencySatisfied, Note: "again"})
	r := ValidateConsistency(att)
	if r.Passed {
		t.Fatal("a duplicated check must fail")
	}
}

func TestConsistencyInvalidStatusIsFlagged(t *testing.T) {
	att := allSatisfied()
	att[2].Status = "maybe"
	r := ValidateConsistency(att)
	if r.Passed {
		t.Fatal("an invalid status must fail")
	}
	if !hasCheck(r.Issues, string(att[2].Check)) {
		t.Fatalf("expected invalid-status issue, got %+v", r.Issues)
	}
}
