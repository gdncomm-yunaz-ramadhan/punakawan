package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/learning"
)

// contextImprovement is one row of the Context Improvements inbox (agent-context
// plan §8). It flattens a learning proposal's envelope together with the live
// status of its underlying artifact review, so the panel can render type,
// rationale, support, evidence, source runs, and conflict/accept status, and
// drive accept/reject through the existing review endpoints (review_id +
// proposal_attempt).
type contextImprovement struct {
	Id              string    `json:"id"`
	ArtifactType    string    `json:"artifact_type"`
	TargetId        string    `json:"target_id"`
	Rationale       string    `json:"rationale,omitempty"`
	SupportCount    int       `json:"support_count"`
	EvidenceIds     []string  `json:"evidence_ids,omitempty"`
	SourceRunIds    []string  `json:"source_run_ids,omitempty"`
	ReviewId        string    `json:"review_id,omitempty"`
	ProposalAttempt int       `json:"proposal_attempt,omitempty"`
	Status          string    `json:"status"`
	CreatedBy       string    `json:"created_by,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ContextImprovementsHandler serves the inbox: every learning proposal for the
// project, newest first, with each row's status overlaid from the live review
// so acceptances/rejections/conflicts made through the review flow show up
// without the side-store having to be kept in lockstep.
func ContextImprovementsHandler(learningStore func() (*learning.Store, error), reviews *artifact.ReviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if learningStore == nil {
			writeError(w, http.StatusInternalServerError, errors.New("api: no learning store configured"))
			return
		}
		store, err := learningStore()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		proposals, err := store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items := make([]contextImprovement, 0, len(proposals))
		for _, p := range proposals {
			row := contextImprovement{
				Id:           p.Id,
				ArtifactType: p.ArtifactType,
				TargetId:     p.TargetId,
				Rationale:    p.Rationale,
				SupportCount: p.SupportCount,
				EvidenceIds:  p.EvidenceIds,
				SourceRunIds: p.SourceRunIds,
				ReviewId:     p.ReviewId,
				Status:       p.Status,
				CreatedBy:    p.CreatedBy,
				CreatedAt:    p.CreatedAt,
				UpdatedAt:    p.UpdatedAt,
			}
			if p.ReviewId != "" && reviews != nil {
				if review, err := reviews.GetReview(p.ReviewId); err == nil {
					row.Status = string(review.Metadata.Status)
					// Learning proposals always open their first (and only)
					// proposal as attempt 1.
					row.ProposalAttempt = 1
				} else if !errors.Is(err, artifact.ErrReviewNotFound) {
					writeError(w, http.StatusInternalServerError, err)
					return
				}
			}
			items = append(items, row)
		}
		writeJSON(w, http.StatusOK, map[string]any{"improvements": items})
	}
}
