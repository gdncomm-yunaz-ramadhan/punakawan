package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// failableStatuses are the review states a "report failure" call may
// legitimately act on - exactly proposableStatuses, the set of statuses
// where a BD task graph is actually in flight (dispatched and not yet
// resolved). draft/submitted have no dispatched run yet to fail, and
// proposal_ready/accepted/rejected/cancelled/conflicted/failed are already
// past the point where an agent is still working - there is nothing left
// for a crashed run to interrupt.
var failableStatuses = proposableStatuses

type failReviewRequest struct {
	Reason string `json:"reason,omitempty"`
}

// FailHandler serves POST /api/v1/reviews/{reviewId}/fail: the explicit,
// symmetric counterpart to CreateProposalHandler's "an agent reports its
// finished revision here" - this is how an agent (or, in the future, a
// supervising process watching a dispatched BD parent task) reports "this
// revision attempt died before producing a proposal," per §18's "Revision
// run fails" recovery case. Like RejectProposalHandler, it never touches
// canonical artifact content - it only updates the review's own
// Metadata.Status.
//
// There is no schema field for a failure reason (ArtifactReview's
// review.instruction is the pre-submission prompt to the agent, not a
// place to record what went wrong afterwards - and once submitted it is
// frozen into the immutable revision request snapshot per §5.3, so
// repurposing it here would be both semantically wrong and blocked by
// UpdateReviewHandler's own draft-only rule). Adding a persisted field
// would mean a schema addition (edit artifactreview.schema.json, then
// `go generate ./...` and `pnpm run generate`) purely to carry a string
// nothing yet reads back - so for this first cut the optional reason is
// only logged server-side, not persisted on the review.
func FailHandler(reviews *artifact.ReviewStore, logger *slog.Logger) http.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		review, err := reviews.GetReview(reviewID)
		if errors.Is(err, artifact.ErrReviewNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		var req failReviewRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Idempotent, matching CancelHandler's precedent (not
		// RejectProposalHandler's, which has no repeat-call concern
		// since accept/reject act on one specific proposal attempt
		// rather than the review as a whole): a supervising process
		// reporting the same crashed run twice (a retried request, or
		// two independent observers noticing the same dead run) must
		// not error just because the first report already landed.
		if review.Metadata.Status == protocol.ArtifactReviewMetadataStatusFailed {
			writeJSON(w, http.StatusOK, review)
			return
		}
		if !failableStatuses[review.Metadata.Status] {
			writeError(w, http.StatusConflict, fmt.Errorf("api: review %s (status %s) has no in-flight revision run to report as failed", reviewID, review.Metadata.Status))
			return
		}

		logger.Info("revision run reported as failed", "review_id", reviewID, "prior_status", review.Metadata.Status, "reason", req.Reason)

		review.Metadata.Status = protocol.ArtifactReviewMetadataStatusFailed
		now := time.Now().UTC()
		review.Metadata.UpdatedAt = &now
		if err := reviews.PutReview(review); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, review)
	}
}
