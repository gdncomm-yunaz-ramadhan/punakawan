package roles

import (
	"fmt"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubmitBagongReview validates and persists Bagong's independent final
// review (§8.4) as a bagong-review knowledge record. Bagong's
// responsibilities explicitly include an "honest confidence statement", so
// honest_summary is required in addition to verdict.
func SubmitBagongReview(store *knowledge.Store, id, title string, review protocol.KnowledgeRecordBagongReview) (protocol.KnowledgeRecord, error) {
	if review.Verdict == nil || *review.Verdict == "" {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: bagong review %s: verdict is required", id)
	}
	if review.HonestSummary == nil || *review.HonestSummary == "" {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: bagong review %s: honest_summary is required (§8.4)", id)
	}

	rec := newSubmissionRecord(id, title, protocol.KnowledgeRecordTypeBagongReview)
	rec.BagongReview = &review
	if err := store.Put(rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: submit bagong review %s: %w", id, err)
	}
	return rec, nil
}
