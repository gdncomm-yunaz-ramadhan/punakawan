package roles

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestSubmitPetrukPlanRequiresRecommendedSolution(t *testing.T) {
	if _, err := SubmitPetrukPlan(nil, "pkw:petruk/ws/run-1", "plan", protocol.KnowledgeRecordPetrukPlan{}); err == nil {
		t.Fatal("expected error for missing recommended_solution")
	}
}

func TestSubmitPetrukPlanPersists(t *testing.T) {
	store := newTestStore(t)

	plan := protocol.KnowledgeRecordPetrukPlan{
		RecommendedSolution: strPtr("Add an idempotency key to the refund endpoint"),
		Alternatives:        []string{"queue-based async refund"},
		ImplementationSteps: []string{"add migration", "implement handler"},
	}

	rec, err := SubmitPetrukPlan(store, "pkw:petruk/ws/run-1", "Petruk plan for refund API", plan)
	if err != nil {
		t.Fatalf("SubmitPetrukPlan: %v", err)
	}
	if rec.Type != protocol.KnowledgeRecordTypePetrukPlan {
		t.Fatalf("Type = %q, want petruk-plan", rec.Type)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PetrukPlan == nil || *got.PetrukPlan.RecommendedSolution != *plan.RecommendedSolution {
		t.Fatalf("PetrukPlan = %+v, want RecommendedSolution %q", got.PetrukPlan, *plan.RecommendedSolution)
	}
}
