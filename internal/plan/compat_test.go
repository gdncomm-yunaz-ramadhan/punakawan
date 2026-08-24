package plan

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestFromFinalPlanRecord(t *testing.T) {
	arch := "event-sourced ledger"
	retrievedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rec := protocol.KnowledgeRecord{
		Id:    "pkw:plan/smoke/run-1",
		Title: "migrate checkout",
		Source: protocol.KnowledgeRecordSource{
			Provider:    "punakawan-mcp",
			RetrievedAt: retrievedAt,
		},
		FinalPlan: &protocol.KnowledgeRecordFinalPlan{
			Requirements:          []string{"r1"},
			AcceptanceCriteria:    []string{"a1"},
			ArchitectureDecision:  &arch,
			VerificationCriteria:  []string{"run integration suite", "manual smoke test"},
			RepositoryImpactMap:   protocol.KnowledgeRecordFinalPlanRepositoryImpactMap{"checkout-api": "high"},
		},
	}

	p, err := FromFinalPlanRecord(rec)
	if err != nil {
		t.Fatalf("FromFinalPlanRecord: %v", err)
	}
	if p.ID != rec.Id {
		t.Fatalf("ID = %q, want %q", p.ID, rec.Id)
	}
	if p.Objective != "migrate checkout" {
		t.Fatalf("Objective = %q, want the record's title", p.Objective)
	}
	if p.ArchitectureDecision != arch {
		t.Fatalf("ArchitectureDecision = %q, want %q", p.ArchitectureDecision, arch)
	}
	if p.Verification != "run integration suite\nmanual smoke test" {
		t.Fatalf("Verification = %q, want the joined verification criteria", p.Verification)
	}
	if p.RepositoryImpactMap["checkout-api"] != "high" {
		t.Fatalf("RepositoryImpactMap[checkout-api] = %q, want %q", p.RepositoryImpactMap["checkout-api"], "high")
	}
	if !p.CreatedAt.Equal(retrievedAt) {
		t.Fatalf("CreatedAt = %v, want %v", p.CreatedAt, retrievedAt)
	}
}

func TestFromFinalPlanRecordRequiresFinalPlanBody(t *testing.T) {
	if _, err := FromFinalPlanRecord(protocol.KnowledgeRecord{Id: "no-body"}); err == nil {
		t.Fatalf("want error for a record with no final_plan body")
	}
}

func TestFromFinalPlanInputValidation(t *testing.T) {
	if _, err := FromFinalPlanInput("id-1", "title", protocol.KnowledgeRecordFinalPlan{
		AcceptanceCriteria: []string{"a1"},
	}); err == nil {
		t.Fatalf("want error when requirements is empty")
	}
	if _, err := FromFinalPlanInput("id-1", "title", protocol.KnowledgeRecordFinalPlan{
		Requirements: []string{"r1"},
	}); err == nil {
		t.Fatalf("want error when acceptance_criteria is empty")
	}
}

func TestFromFinalPlanInputMapsFields(t *testing.T) {
	p, err := FromFinalPlanInput("id-1", "final plan title", protocol.KnowledgeRecordFinalPlan{
		Requirements:       []string{"r1"},
		AcceptanceCriteria: []string{"a1"},
	})
	if err != nil {
		t.Fatalf("FromFinalPlanInput: %v", err)
	}
	if p.ID != "id-1" || p.Objective != "final plan title" {
		t.Fatalf("p = %+v, want id/objective set from the handler's inputs", p)
	}
	if p.Status != "final" {
		t.Fatalf("Status = %q, want %q", p.Status, "final")
	}
}
