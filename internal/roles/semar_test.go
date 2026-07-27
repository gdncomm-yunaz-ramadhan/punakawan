package roles

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestSubmitSemarSynthesisRequiresGoal(t *testing.T) {
	if _, err := SubmitSemarSynthesis(nil, "pkw:synthesis/ws/run-1", "synthesis", protocol.KnowledgeRecordSemarSynthesis{}); err == nil {
		t.Fatal("expected error for missing goal")
	}
}

// TestSubmitSemarSynthesisPreservesUnresolvedDisagreement guards the guardrail
// that Semar synthesizes without hiding disagreement: a blocking open question,
// its observed conflict, and role assumptions must survive the round-trip
// intact rather than being smoothed into a single confident conclusion.
func TestSubmitSemarSynthesisPreservesUnresolvedDisagreement(t *testing.T) {
	store := newTestStore(t)

	synthesis := protocol.KnowledgeRecordSemarSynthesis{
		Goal:        strPtr("Ship idempotent refunds"),
		Assumptions: []string{"Gareng assumes the gateway dedupes; Petruk assumes it does not"},
		OpenQuestions: []protocol.KnowledgeRecordSemarSynthesisOpenQuestionsElem{
			{
				Question:         strPtr("Does the payment gateway dedupe retries?"),
				ObservedConflict: strPtr("Gareng and Petruk disagree on gateway idempotency"),
				Blocking:         boolPtr(true),
			},
		},
	}

	rec, err := SubmitSemarSynthesis(store, "pkw:synthesis/ws/run-9", "Semar synthesis", synthesis)
	if err != nil {
		t.Fatalf("SubmitSemarSynthesis: %v", err)
	}
	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	s := got.SemarSynthesis
	if s == nil || len(s.OpenQuestions) != 1 {
		t.Fatalf("SemarSynthesis = %+v, want one preserved open question", s)
	}
	oq := s.OpenQuestions[0]
	if oq.Blocking == nil || !*oq.Blocking {
		t.Error("blocking flag on the unresolved question was not preserved")
	}
	if oq.ObservedConflict == nil || *oq.ObservedConflict == "" {
		t.Error("the observed conflict behind the disagreement was dropped")
	}
	if len(s.Assumptions) != 1 {
		t.Errorf("Assumptions = %v, want the divergent assumptions preserved", s.Assumptions)
	}
}

func TestSubmitSemarSynthesisPersists(t *testing.T) {
	store := newTestStore(t)

	synthesis := protocol.KnowledgeRecordSemarSynthesis{
		Goal:  strPtr("Ship idempotent refunds"),
		Scope: strPtr("checkout-platform refund API only"),
		OpenQuestions: []protocol.KnowledgeRecordSemarSynthesisOpenQuestionsElem{
			{
				Question: strPtr("Should refunds above $500 require manual approval?"),
				Blocking: boolPtr(true),
			},
		},
	}

	rec, err := SubmitSemarSynthesis(store, "pkw:synthesis/ws/run-1", "Semar synthesis for refund API", synthesis)
	if err != nil {
		t.Fatalf("SubmitSemarSynthesis: %v", err)
	}
	if rec.Type != protocol.KnowledgeRecordTypeSemarSynthesis {
		t.Fatalf("Type = %q, want semar-synthesis", rec.Type)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SemarSynthesis == nil || len(got.SemarSynthesis.OpenQuestions) != 1 {
		t.Fatalf("SemarSynthesis = %+v, want one open question", got.SemarSynthesis)
	}
}

func TestSubmitFinalPlanRequiresRequirementsAndAcceptanceCriteria(t *testing.T) {
	if _, err := SubmitFinalPlan(nil, "pkw:plan/ws/run-1", "plan", protocol.KnowledgeRecordFinalPlan{}); err == nil {
		t.Fatal("expected error for missing requirements/acceptance_criteria")
	}
	if _, err := SubmitFinalPlan(nil, "pkw:plan/ws/run-1", "plan", protocol.KnowledgeRecordFinalPlan{
		Requirements: []string{"refunds must be idempotent"},
	}); err == nil {
		t.Fatal("expected error for missing acceptance_criteria")
	}
}

func TestSubmitFinalPlanPersists(t *testing.T) {
	store := newTestStore(t)

	plan := protocol.KnowledgeRecordFinalPlan{
		Requirements:       []string{"refunds must be idempotent"},
		AcceptanceCriteria: []string{"duplicate refund requests return the original result"},
		RepositoryImpactMap: protocol.KnowledgeRecordFinalPlanRepositoryImpactMap{
			"checkout-api": "add idempotency key column",
		},
	}

	rec, err := SubmitFinalPlan(store, "pkw:plan/ws/run-1", "Final plan for refund API", plan)
	if err != nil {
		t.Fatalf("SubmitFinalPlan: %v", err)
	}
	if rec.Type != protocol.KnowledgeRecordTypeFinalPlan {
		t.Fatalf("Type = %q, want final-plan", rec.Type)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FinalPlan == nil || got.FinalPlan.RepositoryImpactMap["checkout-api"] != "add idempotency key column" {
		t.Fatalf("FinalPlan = %+v, want checkout-api impact set", got.FinalPlan)
	}
}

func boolPtr(b bool) *bool { return &b }
