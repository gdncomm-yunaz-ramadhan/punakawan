package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// parseAttempt reads the {proposalId} path value as an attempt number - the
// only shape a proposal id takes (attempts are numbered densely from 1).
func parseAttempt(r *http.Request) (int, error) {
	return strconv.Atoi(r.PathValue("proposalId"))
}

// AcceptProposalHandler serves
// POST /api/v1/projects/{projectId}/reviews/{reviewId}/proposals/{proposalId}/accept.
// It backs the Context Improvements inbox's accept action (a learning
// proposal is itself an artifact review) - stores dispatches
// review.Artifact.Type to the matching artifact.Store, whose CreateVersion
// performs the acceptance.
func AcceptProposalHandler(reviews *artifact.ReviewStore, stores ArtifactStores) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		attempt, err := parseAttempt(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: proposalId must be an attempt number"))
			return
		}
		content, proposal, err := reviews.GetProposal(reviewID, attempt)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		review, err := reviews.GetReview(reviewID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		store, err := storeFor(stores, review.Artifact.Type)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		// Serialize this whole read-compare-write sequence per artifact id: the
		// conflict check ("current canonical hash == proposal base hash") and
		// the version it creates on success must be atomic together, or two
		// concurrent accepts against two different reviews' proposals - both
		// based on the same now-current version - can each read Current()
		// before either has written the next version, both see current==base,
		// and both succeed onto the same base. store.LockArtifact (not a
		// package-level lock) so distinct artifact ids serialize independently
		// of each other.
		defer store.LockArtifact(proposal.Base.ArtifactId)()

		current, err := store.Current(proposal.Base.ArtifactId)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if current.RevisionHash != proposal.Base.RevisionHash {
			review.Metadata.Status = protocol.ArtifactReviewMetadataStatusConflicted
			now := time.Now().UTC()
			review.Metadata.UpdatedAt = &now
			_ = reviews.PutReview(review)
			writeError(w, http.StatusConflict, fmt.Errorf("api: canonical artifact changed since this proposal's base (now version %d) - rebase required", current.Version))
			return
		}
		if proposal.Results != nil && proposal.Results.ValidationStatus != nil && *proposal.Results.ValidationStatus == protocol.ArtifactRevisionProposalResultsValidationStatusFailed {
			writeError(w, http.StatusConflict, fmt.Errorf("api: proposal failed validation and cannot be accepted"))
			return
		}

		newRef, err := store.CreateVersion(proposal.Base.ArtifactId, review.Metadata.WorkspaceId, content, time.Now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		review.Metadata.Status = protocol.ArtifactReviewMetadataStatusAccepted
		now := time.Now().UTC()
		review.Metadata.UpdatedAt = &now
		if err := reviews.PutReview(review); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"review": review, "new_version": newRef})
	}
}

// RejectProposalHandler serves
// POST /api/v1/projects/{projectId}/reviews/{reviewId}/proposals/{proposalId}/reject.
// It backs the Context Improvements inbox's reject action. Rejection never
// touches canonical artifact state - it only updates the review's own
// status.
func RejectProposalHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		attempt, err := parseAttempt(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: proposalId must be an attempt number"))
			return
		}
		if _, _, err := reviews.GetProposal(reviewID, attempt); err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		review, err := reviews.GetReview(reviewID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		review.Metadata.Status = protocol.ArtifactReviewMetadataStatusRejected
		now := time.Now().UTC()
		review.Metadata.UpdatedAt = &now
		if err := reviews.PutReview(review); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, review)
	}
}
