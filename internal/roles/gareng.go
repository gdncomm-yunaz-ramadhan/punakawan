package roles

import (
	"fmt"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubmitGarengReview validates and persists Gareng's feasibility and risk
// review (§8.2) as a gareng-review knowledge record.
func SubmitGarengReview(store *knowledge.Store, id, title string, review protocol.KnowledgeRecordGarengReview) (protocol.KnowledgeRecord, error) {
	if review.Verdict == nil || *review.Verdict == "" {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: gareng review %s: verdict is required", id)
	}

	rec := newSubmissionRecord(id, title, protocol.KnowledgeRecordTypeGarengReview)
	rec.GarengReview = &review
	if err := store.Put(rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: submit gareng review %s: %w", id, err)
	}
	return rec, nil
}
