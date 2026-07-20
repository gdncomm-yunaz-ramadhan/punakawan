package roles

import (
	"fmt"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubmitPetrukPlan validates and persists Petruk's usefulness challenge and
// implementation planning output (§8.3, planning only — task execution is
// separate, later work) as a petruk-plan knowledge record.
func SubmitPetrukPlan(store *knowledge.Store, id, title string, plan protocol.KnowledgeRecordPetrukPlan) (protocol.KnowledgeRecord, error) {
	if plan.RecommendedSolution == nil || *plan.RecommendedSolution == "" {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: petruk plan %s: recommended_solution is required", id)
	}

	rec := newSubmissionRecord(id, title, protocol.KnowledgeRecordTypePetrukPlan)
	rec.PetrukPlan = &plan
	if err := store.Put(rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: submit petruk plan %s: %w", id, err)
	}
	return rec, nil
}
