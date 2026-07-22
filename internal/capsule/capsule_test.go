package capsule

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func baseCapsule() protocol.ContextCapsule {
	return protocol.ContextCapsule{
		Id:                 "cap-1",
		TaskId:             "bd-task-1",
		CreatedAt:          time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC),
		Role:               protocol.ContextCapsuleRolePetruk,
		Objective:          "Implement the refund flow",
		AcceptanceCriteria: []string{"Refund settles same day"},
		Constraints:        []string{"Do not change the approval flow"},
		Requirements: []protocol.ContextCapsuleRequirementsElem{
			{Id: "pkw:req/smoke/REQ-1"},
		},
		RelevantKnowledge: []protocol.ContextCapsuleRelevantKnowledgeElem{
			{Id: "pkw:decision/smoke/DEC-1"},
		},
		Evidence: []protocol.ContextCapsuleEvidenceElem{
			{Id: "ev-1"},
		},
		AllowedTools:     []string{"write_file", "run_tests"},
		ForbiddenActions: []string{"push"},
	}
}

func TestDigestIsDeterministicForIdenticalInput(t *testing.T) {
	a := baseCapsule()
	b := baseCapsule()

	da, err := Digest(a)
	if err != nil {
		t.Fatalf("Digest(a): %v", err)
	}
	db, err := Digest(b)
	if err != nil {
		t.Fatalf("Digest(b): %v", err)
	}
	if da != db {
		t.Fatalf("Digest = %q and %q for identical capsules, want equal", da, db)
	}
}

func TestDigestIgnoresNonSubstantiveFields(t *testing.T) {
	a := baseCapsule()
	b := baseCapsule()
	b.Id = "cap-2"
	b.TaskId = "bd-task-2"
	b.CreatedAt = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	expected := "produce something"
	b.ExpectedOutput = &expected
	budget := 5000
	b.TokenBudget = &budget
	b.Assumptions = []string{"the gateway is reachable"}
	b.UnresolvedQuestions = []string{"is retry allowed?"}

	da, err := Digest(a)
	if err != nil {
		t.Fatalf("Digest(a): %v", err)
	}
	db, err := Digest(b)
	if err != nil {
		t.Fatalf("Digest(b): %v", err)
	}
	if da != db {
		t.Fatalf("Digest changed when only id/task_id/created_at/expected_output/token_budget/assumptions/unresolved_questions differed: %q != %q", da, db)
	}
}

func TestDigestChangesWithSubstantiveField(t *testing.T) {
	a := baseCapsule()
	b := baseCapsule()
	b.Objective = "Implement a different refund flow"

	da, _ := Digest(a)
	db, _ := Digest(b)
	if da == db {
		t.Fatal("Digest did not change when Objective changed")
	}
}

func TestDigestTreatsNilAndEmptySliceAsEquivalent(t *testing.T) {
	a := baseCapsule()
	a.Assumptions = nil // not part of the digest anyway, but exercise a digest-relevant field too
	a.Constraints = nil

	b := baseCapsule()
	b.Constraints = []string{}

	da, err := Digest(a)
	if err != nil {
		t.Fatalf("Digest(a): %v", err)
	}
	db, err := Digest(b)
	if err != nil {
		t.Fatalf("Digest(b): %v", err)
	}
	if da != db {
		t.Fatalf("Digest = %q (nil Constraints) vs %q (empty Constraints), want equal", da, db)
	}
}

func TestDigestHasSha256Format(t *testing.T) {
	d, err := Digest(baseCapsule())
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if len(d) != len("sha256:")+64 || d[:7] != "sha256:" {
		t.Fatalf("Digest = %q, want sha256:<64 hex chars>", d)
	}
}
