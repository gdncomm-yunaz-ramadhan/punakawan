package roles

import (
	"fmt"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubmitSemarSynthesis validates and persists Semar's consolidated output
// after merging Gareng and Petruk findings (§8.1), including any
// clarification questions to raise (§9.2), as a semar-synthesis knowledge
// record.
func SubmitSemarSynthesis(store *knowledge.Store, id, title string, synthesis protocol.KnowledgeRecordSemarSynthesis) (protocol.KnowledgeRecord, error) {
	if synthesis.Goal == nil || *synthesis.Goal == "" {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: semar synthesis %s: goal is required", id)
	}

	rec := newSubmissionRecord(id, title, protocol.KnowledgeRecordTypeSemarSynthesis)
	rec.SemarSynthesis = &synthesis
	if err := store.Put(rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: submit semar synthesis %s: %w", id, err)
	}
	return rec, nil
}

// SubmitFinalPlan validates and persists Semar's final implementation plan
// (§9.3) as a final-plan knowledge record. This is a distinct artifact from
// SubmitSemarSynthesis, produced at a later workflow stage once no further
// clarification is required; the MCP submit_semar_synthesis tool (§28.4)
// calls whichever of the two matches the payload the client submitted.
func SubmitFinalPlan(store *knowledge.Store, id, title string, plan protocol.KnowledgeRecordFinalPlan) (protocol.KnowledgeRecord, error) {
	if len(plan.Requirements) == 0 {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: final plan %s: requirements must have at least one entry", id)
	}
	if len(plan.AcceptanceCriteria) == 0 {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: final plan %s: acceptance_criteria must have at least one entry", id)
	}

	rec := newSubmissionRecord(id, title, protocol.KnowledgeRecordTypeFinalPlan)
	rec.FinalPlan = &plan
	if err := store.Put(rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: submit final plan %s: %w", id, err)
	}
	return rec, nil
}
