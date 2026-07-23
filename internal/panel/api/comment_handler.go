package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// anchorKindForArtifactType is the one anchor.kind each artifact type
// accepts - a markdown plan anchors against Markdown blocks, a
// retrieval_recipe anchors against a structured field_path into its JSON
// serialization (punakawan-procedural-knowledge-retrieval-recipe-plan-final.md
// Phase 5). Both still go through the single artifact.ResolveAnchor entry
// point (dispatching on anchor.Kind itself), not a parallel resolver.
var anchorKindForArtifactType = map[protocol.ArtifactReviewArtifactType]protocol.ArtifactCommentAnchorKind{
	protocol.ArtifactReviewArtifactTypePlan:            protocol.ArtifactCommentAnchorKindMarkdownBlock,
	protocol.ArtifactReviewArtifactTypeRetrievalRecipe: protocol.ArtifactCommentAnchorKindRecipeFieldPath,
}

// resolveCommentAnchor validates anchor against the exact content the
// review is pinned to, per §6's resolution chain: a comment whose base
// revision hash no longer matches the reviewed version, or whose anchor
// cannot be resolved against it at all, is rejected up front rather than
// silently persisted pointing at nothing. stores dispatches
// review.Artifact.Type to the matching artifact.Store so both artifact
// types share this one validation path.
func resolveCommentAnchor(stores ArtifactStores, review protocol.ArtifactReview, anchor protocol.ArtifactCommentAnchor) error {
	store, err := storeFor(stores, review.Artifact.Type)
	if err != nil {
		return err
	}
	if anchor.BaseRevisionHash != review.Artifact.RevisionHash {
		return fmt.Errorf("api: anchor's base_revision_hash does not match the reviewed version %d - refresh and re-anchor", review.Artifact.Version)
	}
	content, _, err := store.Version(review.Artifact.Id, review.Artifact.Version)
	if err != nil {
		return err
	}
	if _, method := artifact.ResolveAnchor(string(content), anchor); method == artifact.AnchorConflicted {
		return fmt.Errorf("api: anchor does not resolve against version %d's content", review.Artifact.Version)
	}
	return nil
}

func editableReview(reviews *artifact.ReviewStore, reviewID string) (protocol.ArtifactReview, error) {
	review, err := reviews.GetReview(reviewID)
	if err != nil {
		return protocol.ArtifactReview{}, err
	}
	if review.Metadata.Status != protocol.ArtifactReviewMetadataStatusDraft {
		return protocol.ArtifactReview{}, fmt.Errorf("api: review %s is no longer a draft and cannot be commented on", reviewID)
	}
	return review, nil
}

type createCommentRequest struct {
	// Id lets a client supply its own comment id so a retried create
	// request is idempotent (re-posting the same id with the same body
	// just re-appends an identical history entry - LatestComments still
	// folds down to one logical comment, per §5.2/§14's "idempotent
	// comment ops").
	Id     string                         `json:"id,omitempty"`
	Anchor protocol.ArtifactCommentAnchor `json:"anchor"`
	Body   string                         `json:"body"`
}

// CreateCommentHandler serves POST /api/v1/reviews/{reviewId}/comments.
func CreateCommentHandler(reviews *artifact.ReviewStore, stores ArtifactStores) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		review, err := editableReview(reviews, reviewID)
		if errors.Is(err, artifact.ErrReviewNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusConflict, err)
			return
		}

		var req createCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Body == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: comment body is required"))
			return
		}
		if wantKind, ok := anchorKindForArtifactType[review.Artifact.Type]; !ok || req.Anchor.Kind != wantKind {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: anchor.kind must be %q for artifact type %q", anchorKindForArtifactType[review.Artifact.Type], review.Artifact.Type))
			return
		}
		if err := resolveCommentAnchor(stores, review, req.Anchor); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}

		commentID := req.Id
		if commentID == "" {
			id, err := newID("comment")
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			commentID = id
		}

		comment := protocol.ArtifactComment{
			Id:       commentID,
			ReviewId: reviewID,
			Author:   reviewerIdentity,
			Status:   protocol.ArtifactCommentStatusOpen,
			Anchor:   req.Anchor,
			Body:     req.Body,
		}
		if err := reviews.AppendComment(reviewID, comment); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, comment)
	}
}

// CommentsHandler serves GET /api/v1/reviews/{reviewId}/comments - the
// latest state of every comment (including obsolete ones, so the client
// can render a "deleted" tombstone rather than have items silently
// vanish), per §14's comment inspection needs.
func CommentsHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		if _, err := reviews.GetReview(reviewID); errors.Is(err, artifact.ErrReviewNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		comments, err := reviews.LatestComments(reviewID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": comments})
	}
}

func latestCommentByID(reviews *artifact.ReviewStore, reviewID, commentID string) (protocol.ArtifactComment, error) {
	comments, err := reviews.LatestComments(reviewID)
	if err != nil {
		return protocol.ArtifactComment{}, err
	}
	for _, c := range comments {
		if c.Id == commentID {
			return c, nil
		}
	}
	return protocol.ArtifactComment{}, fmt.Errorf("%w: comment %s", artifact.ErrReviewNotFound, commentID)
}

type updateCommentRequest struct {
	Body   *string                         `json:"body,omitempty"`
	Status *protocol.ArtifactCommentStatus `json:"status,omitempty"`
}

var validCommentStatuses = map[protocol.ArtifactCommentStatus]bool{
	protocol.ArtifactCommentStatusOpen:               true,
	protocol.ArtifactCommentStatusAddressed:          true,
	protocol.ArtifactCommentStatusPartiallyAddressed: true,
	protocol.ArtifactCommentStatusRejectedByAgent:    true,
	protocol.ArtifactCommentStatusResolvedByUser:     true,
	protocol.ArtifactCommentStatusNeedsClarification: true,
	protocol.ArtifactCommentStatusObsolete:           true,
}

// UpdateCommentHandler serves
// PATCH /api/v1/reviews/{reviewId}/comments/{commentId}: it appends a new
// history entry with the requested edits applied over the current latest
// entry - re-sending the same edit twice appends the same content twice,
// which folds to the same logical result, so retries are safe.
func UpdateCommentHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		commentID := r.PathValue("commentId")

		if _, err := editableReview(reviews, reviewID); err != nil {
			status := http.StatusConflict
			if errors.Is(err, artifact.ErrReviewNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}

		comment, err := latestCommentByID(reviews, reviewID, commentID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}

		var req updateCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.Body == nil && req.Status == nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: at least one of body or status is required"))
			return
		}
		if req.Body != nil {
			if *req.Body == "" {
				writeError(w, http.StatusBadRequest, fmt.Errorf("api: comment body cannot be empty"))
				return
			}
			comment.Body = *req.Body
		}
		if req.Status != nil {
			if !validCommentStatuses[*req.Status] {
				writeError(w, http.StatusBadRequest, fmt.Errorf("api: unknown comment status %q", *req.Status))
				return
			}
			comment.Status = *req.Status
		}

		if err := reviews.AppendComment(reviewID, comment); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, comment)
	}
}

// DeleteCommentHandler serves
// DELETE /api/v1/reviews/{reviewId}/comments/{commentId}. Comments live
// in an append-only ledger, so "delete" means appending an obsolete
// status entry rather than removing history - deleting an already-
// obsolete (or never-existent) comment is a no-op success, per §14's
// "idempotent comment ops".
func DeleteCommentHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		commentID := r.PathValue("commentId")

		if _, err := editableReview(reviews, reviewID); err != nil {
			status := http.StatusConflict
			if errors.Is(err, artifact.ErrReviewNotFound) {
				status = http.StatusNotFound
			}
			writeError(w, status, err)
			return
		}

		comment, err := latestCommentByID(reviews, reviewID, commentID)
		if err != nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if comment.Status == protocol.ArtifactCommentStatusObsolete {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		comment.Status = protocol.ArtifactCommentStatusObsolete
		if err := reviews.AppendComment(reviewID, comment); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
