package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/revision"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// commentSnapshotHash hashes latest's comments deterministically (sorted
// by id, so append-order quirks never change the hash for the same
// logical comment set) - one of §8's four idempotency-key inputs.
func commentSnapshotHash(latest []protocol.ArtifactComment) (string, error) {
	sorted := make([]protocol.ArtifactComment, len(latest))
	copy(sorted, latest)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Id < sorted[j].Id })
	data, err := json.Marshal(sorted)
	if err != nil {
		return "", err
	}
	return artifact.Hash(data), nil
}

type runReferenceResponse struct {
	RunID        string `json:"run_id"`
	ParentTaskID string `json:"parent_task_id"`
}

type submitResponse struct {
	RevisionRequest protocol.ArtifactRevisionRequest `json:"revision_request"`
	Run             runReferenceResponse             `json:"run"`
}

// SubmitHandler serves POST /api/v1/reviews/{reviewId}/submit: §8's
// Automatic Retrigger Flow. It freezes the review's current comment
// snapshot into an immutable ArtifactRevisionRequest, then hands it to
// dispatcher to create a durable BD task graph an agent later claims.
//
// The request's own id is §8's idempotency key (review id + base
// revision hash + comment snapshot hash + a fixed sequence of 1 - this
// phase only handles a review's first submission; a later "request
// changes" attempt-2 flow is a follow-up phase's concern). A second call
// with an unchanged review/comment state resolves to the exact same
// request and re-dispatches (itself idempotent), rather than creating a
// competing run - this is also how a panel restart after "submit
// succeeded but the process died before responding" recovers: the next
// submit call (or an automatic retry) completes cleanly.
func SubmitHandler(reviews *artifact.ReviewStore, dispatcher revision.Dispatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")

		// Serialize this whole read-check-write sequence per review id:
		// two concurrent submits for the *same* review (a double-click,
		// or a client retrying after a lost response) must not both
		// observe "no submission exists yet for this idempotency key"
		// and both proceed to create one, per §8's "submitting twice
		// must return the existing run, not create two competing
		// agents."
		defer reviews.LockReview(reviewID)()

		review, err := reviews.GetReview(reviewID)
		if errors.Is(err, artifact.ErrReviewNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		latest, err := reviews.LatestComments(reviewID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		snapshotHash, err := commentSnapshotHash(latest)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		requestID := revision.IdempotencyKey(reviewID, review.Artifact.RevisionHash, snapshotHash, 1)

		revReq, err := reviews.GetRevisionRequest(reviewID, requestID)
		fresh := errors.Is(err, os.ErrNotExist)
		if err != nil && !fresh {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		if fresh {
			if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusDraft {
				writeError(w, http.StatusConflict, fmt.Errorf("api: review %s is not a draft and has no matching pending submission", reviewID))
				return
			}
			revReq = protocol.ArtifactRevisionRequest{
				Metadata: protocol.ArtifactRevisionRequestMetadata{
					Id:          requestID,
					ReviewId:    reviewID,
					SubmittedAt: time.Now().UTC(),
					SubmittedBy: reviewerIdentity,
				},
				BaseArtifact: protocol.ArtifactRevisionRequestBaseArtifact{
					Type:         protocol.ArtifactRevisionRequestBaseArtifactType(review.Artifact.Type),
					Id:           review.Artifact.Id,
					Version:      review.Artifact.Version,
					RevisionHash: review.Artifact.RevisionHash,
				},
				Workflow: protocol.ArtifactRevisionRequestWorkflow{Type: protocol.ArtifactRevisionRequestWorkflowTypeRevisePlanFromReview},
				Comments: protocol.ArtifactRevisionRequestComments{SnapshotHash: snapshotHash, Count: len(latest)},
			}
			if err := reviews.PutRevisionRequest(revReq); err != nil && !errors.Is(err, artifact.ErrRevisionRequestExists) {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}

		ref, err := dispatcher.Dispatch(r.Context(), revision.Request{
			RequestID:         requestID,
			ReviewID:          reviewID,
			ArtifactType:      string(review.Artifact.Type),
			ArtifactID:        review.Artifact.Id,
			BaseVersion:       review.Artifact.Version,
			BaseRevisionHash:  review.Artifact.RevisionHash,
			ReviewTitle:       review.Review.Title,
			ReviewInstruction: derefOr(review.Review.Instruction, ""),
			CommentCount:      len(latest),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		if fresh {
			review.Metadata.Status = protocol.ArtifactReviewMetadataStatusQueued
			now := time.Now().UTC()
			review.Metadata.UpdatedAt = &now
			if err := reviews.PutReview(review); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}

		status := http.StatusOK
		if fresh {
			status = http.StatusCreated
		}
		writeJSON(w, status, submitResponse{
			RevisionRequest: revReq,
			Run:             runReferenceResponse{RunID: ref.RunID, ParentTaskID: ref.ParentTaskID},
		})
	}
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

// cancellableStatuses are every review status Cancel is allowed to act
// on - anything already at a terminal outcome (accepted/rejected/failed)
// is left alone; cancelling an already-cancelled review is a harmless
// no-op (idempotent per §14).
var cancellableStatuses = map[protocol.ArtifactReviewMetadataStatus]bool{
	protocol.ArtifactReviewMetadataStatusDraft:                 true,
	protocol.ArtifactReviewMetadataStatusSubmitted:             true,
	protocol.ArtifactReviewMetadataStatusQueued:                true,
	protocol.ArtifactReviewMetadataStatusRevising:              true,
	protocol.ArtifactReviewMetadataStatusAwaitingClarification: true,
	protocol.ArtifactReviewMetadataStatusProposalReady:         true,
	protocol.ArtifactReviewMetadataStatusRevisionRequested:     true,
	protocol.ArtifactReviewMetadataStatusConflicted:            true,
}

// CancelHandler serves POST /api/v1/reviews/{reviewId}/cancel.
func CancelHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
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

		if review.Metadata.Status == protocol.ArtifactReviewMetadataStatusCancelled {
			writeJSON(w, http.StatusOK, review)
			return
		}
		if !cancellableStatuses[review.Metadata.Status] {
			writeError(w, http.StatusConflict, fmt.Errorf("api: review %s is already %s and cannot be cancelled", reviewID, review.Metadata.Status))
			return
		}

		review.Metadata.Status = protocol.ArtifactReviewMetadataStatusCancelled
		now := time.Now().UTC()
		review.Metadata.UpdatedAt = &now
		if err := reviews.PutReview(review); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, review)
	}
}

type timelineResponse struct {
	Review          protocol.ArtifactReview           `json:"review"`
	CommentCount    int                               `json:"comment_count"`
	RevisionRequest *protocol.ArtifactRevisionRequest `json:"revision_request,omitempty"`
	Run             *runReferenceResponse             `json:"run,omitempty"`
}

// TimelineHandler serves GET /api/v1/reviews/{reviewId}/timeline: the
// full current status of a review's submission lifecycle in one read, so
// a panel restart (§18's "panel stops after submission") recovers simply
// by re-fetching this - there is no separate live/durable state to
// reconnect to, since RunReference.RunID is always recomputed from the
// same deterministic id the revision request itself carries.
func TimelineHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
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
		latest, err := reviews.LatestComments(reviewID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		resp := timelineResponse{Review: review, CommentCount: len(latest)}

		snapshotHash, err := commentSnapshotHash(latest)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		requestID := revision.IdempotencyKey(reviewID, review.Artifact.RevisionHash, snapshotHash, 1)
		if revReq, err := reviews.GetRevisionRequest(reviewID, requestID); err == nil {
			resp.RevisionRequest = &revReq
			resp.Run = &runReferenceResponse{RunID: revReq.Metadata.Id, ParentTaskID: revReq.Metadata.Id}
		} else if !errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
