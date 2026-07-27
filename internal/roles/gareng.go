package roles

import (
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubmitGarengReview validates and persists Gareng's feasibility and risk
// review (§8.2) as a gareng-review knowledge record.
func SubmitGarengReview(store *knowledge.Store, id, title string, review protocol.KnowledgeRecordGarengReview) (protocol.KnowledgeRecord, error) {
	if review.Verdict == nil || *review.Verdict == "" {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: gareng review %s: verdict is required", id)
	}

	// A blocking finding must carry backing evidence or a concrete failure
	// scenario; otherwise it is a risk or an assumption, not a blocker. Each
	// blocking_findings entry must be non-empty, and at least one
	// required_evidence entry must exist to justify halting the workflow (the
	// prompt asks the caller to record the backing evidence there).
	if hasNonEmpty(review.BlockingFindings) && !hasNonEmpty(review.RequiredEvidence) {
		return protocol.KnowledgeRecord{}, fmt.Errorf(
			"roles: gareng review %s: a blocking finding requires evidence or a concrete failure scenario in required_evidence", id)
	}

	rec := newSubmissionRecord(id, title, protocol.KnowledgeRecordTypeGarengReview)
	rec.GarengReview = &review
	if err := store.Put(rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("roles: submit gareng review %s: %w", id, err)
	}
	return rec, nil
}

// hasNonEmpty reports whether the slice contains at least one entry with
// non-whitespace content.
func hasNonEmpty(items []string) bool {
	for _, s := range items {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}
