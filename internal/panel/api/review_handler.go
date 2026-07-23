package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// reviewerIdentity is the honest placeholder for §5.1's created_by/author
// fields: the panel has no multi-user identity system, only a single
// local operator, so every review/comment this process writes is
// attributed to this fixed string rather than fabricating a name.
const reviewerIdentity = "local"

type createReviewRequest struct {
	Title       string  `json:"title"`
	Instruction *string `json:"instruction,omitempty"`
}

// CreateReviewHandler serves POST /api/v1/artifacts/{type}/{id}/reviews:
// it opens a new draft review pinned to the artifact's current version,
// per §5.1. stores dispatches {type} to the matching artifact.Store (see
// resolveArtifactType), so "plan" and "retrieval_recipe" share this one
// handler instead of a second, recipe-specific copy.
func CreateReviewHandler(stores ArtifactStores, reviews *artifact.ReviewStore, workspaceID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, artifactType, err := resolveArtifactType(stores, r.PathValue("type"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		artifactID := r.PathValue("id")

		var req createReviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Title == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("title is required"))
			return
		}

		ref, err := store.Current(artifactID)
		if isArtifactNotFound(err) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		reviewID, err := newID("review")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		review := protocol.ArtifactReview{
			Metadata: protocol.ArtifactReviewMetadata{
				Id:          reviewID,
				WorkspaceId: workspaceID,
				Status:      protocol.ArtifactReviewMetadataStatusDraft,
				CreatedBy:   reviewerIdentity,
				CreatedAt:   time.Now().UTC(),
			},
			Artifact: protocol.ArtifactReviewArtifact{
				Type:         artifactType,
				Id:           artifactID,
				Version:      ref.Version,
				RevisionHash: ref.RevisionHash,
			},
			Review: protocol.ArtifactReviewReview{
				Title:       req.Title,
				Instruction: req.Instruction,
			},
		}
		if err := reviews.PutReview(review); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, review)
	}
}

// ReviewHandler serves GET /api/v1/reviews/{reviewId}.
func ReviewHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		review, err := reviews.GetReview(r.PathValue("reviewId"))
		if errors.Is(err, artifact.ErrReviewNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, review)
	}
}

type updateReviewRequest struct {
	Title       *string `json:"title,omitempty"`
	Instruction *string `json:"instruction,omitempty"`
}

// UpdateReviewHandler serves PATCH /api/v1/reviews/{reviewId}: it edits
// the review's own title/instruction fields (the general review
// instruction textarea from §13.10's review mode). Only a still-draft
// review is editable - once submitted, its instruction is part of an
// immutable revision request snapshot instead, per §5.3.
func UpdateReviewHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
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
		if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusDraft {
			writeError(w, http.StatusConflict, fmt.Errorf("review %s is no longer a draft and cannot be edited", reviewID))
			return
		}

		var req updateReviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Title != nil {
			if *req.Title == "" {
				writeError(w, http.StatusBadRequest, fmt.Errorf("title cannot be empty"))
				return
			}
			review.Review.Title = *req.Title
		}
		if req.Instruction != nil {
			review.Review.Instruction = req.Instruction
		}
		now := time.Now().UTC()
		review.Metadata.UpdatedAt = &now

		if err := reviews.PutReview(review); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, review)
	}
}
