package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/revision"
	"github.com/ygrip/punakawan/internal/validation"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// proposableStatuses are the review states a proposal may legitimately be
// submitted against - after submission and before a final accept/reject
// decision.
var proposableStatuses = map[protocol.ArtifactReviewMetadataStatus]bool{
	protocol.ArtifactReviewMetadataStatusQueued:                true,
	protocol.ArtifactReviewMetadataStatusRevising:              true,
	protocol.ArtifactReviewMetadataStatusAwaitingClarification: true,
	protocol.ArtifactReviewMetadataStatusRevisionRequested:     true,
}

// nextAttempt returns the smallest attempt number reviewID has no stored
// proposal for yet.
func nextAttempt(reviews *artifact.ReviewStore, reviewID string) int {
	attempt := 1
	for {
		if _, _, err := reviews.GetProposal(reviewID, attempt); err != nil {
			return attempt
		}
		attempt++
	}
}

// currentRevisionRequestID recomputes the same deterministic id
// SubmitHandler derived at submission time - it doubles as the BD run id,
// so no separate lookup/storage is needed.
func currentRevisionRequestID(reviews *artifact.ReviewStore, review protocol.ArtifactReview) (string, error) {
	latest, err := reviews.LatestComments(review.Metadata.Id)
	if err != nil {
		return "", err
	}
	snapshotHash, err := commentSnapshotHash(latest)
	if err != nil {
		return "", err
	}
	return revision.IdempotencyKey(review.Metadata.Id, review.Artifact.RevisionHash, snapshotHash, 1), nil
}

type createProposalRequest struct {
	Content            string                                                           `json:"content"`
	ChangeSummary      string                                                           `json:"change_summary,omitempty"`
	CommentResolutions []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem `json:"comment_resolutions,omitempty"`
	// ConsistencyAttestations is the revising agent's §11 consistency
	// self-report (punokawan-apy.6.1). It is optional on the wire so existing
	// callers keep working, but when supplied it is validated for completeness
	// and any declared violation blocks acceptance.
	ConsistencyAttestations []validation.ConsistencyAttestation `json:"consistency_attestations,omitempty"`
}

type createProposalResponse struct {
	Proposal    protocol.ArtifactRevisionProposal `json:"proposal"`
	Structural  validation.StructuralReport       `json:"structural"`
	Compliance  validation.ReviewComplianceReport `json:"compliance"`
	Consistency validation.ConsistencyReport      `json:"consistency"`
}

// CreateProposalHandler serves POST /api/v1/reviews/{reviewId}/proposals:
// whichever agent claimed the dispatched BD task graph reports its
// finished revision here. It is the concrete answer to the gap
// punokawan-apy.5.1 filed - the "Generate diff and resolution report"
// child task's output lands here. stores dispatches review.Artifact.Type
// to the matching artifact.Store.
func CreateProposalHandler(reviews *artifact.ReviewStore, stores ArtifactStores) http.HandlerFunc {
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
		if !proposableStatuses[review.Metadata.Status] {
			writeError(w, http.StatusConflict, fmt.Errorf("api: review %s (status %s) cannot accept a proposal right now", reviewID, review.Metadata.Status))
			return
		}

		var req createProposalRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: content is required"))
			return
		}

		store, err := storeFor(stores, review.Artifact.Type)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		baseContent, _, err := store.Version(review.Artifact.Id, review.Artifact.Version)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		proposedVersion := review.Artifact.Version + 1
		structural := validation.ValidateStructure(string(baseContent), req.Content, review.Artifact.Version, proposedVersion)
		latestComments, err := reviews.LatestComments(reviewID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		compliance := validation.ValidateReviewCompliance(latestComments, req.CommentResolutions)
		consistency := validation.ValidateConsistency(req.ConsistencyAttestations)

		validationStatus := protocol.ArtifactRevisionProposalResultsValidationStatusPassed
		// A missing self-report (Attested=false) is surfaced but does not yet
		// hard-block, so existing callers keep working; once the agent DOES
		// attest, an incomplete report or a declared violation fails validation
		// (punokawan-apy.6.1). Making attestation itself mandatory is a
		// deliberate follow-up.
		consistencyBlocks := consistency.Attested && !consistency.Passed
		if !structural.Passed || !compliance.Passed || consistencyBlocks {
			validationStatus = protocol.ArtifactRevisionProposalResultsValidationStatusFailed
		}

		revisionRequestID, err := currentRevisionRequestID(reviews, review)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		attempt := nextAttempt(reviews, reviewID)
		proposalID, err := newID("proposal")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		var addressed, partial int
		for _, res := range req.CommentResolutions {
			switch res.Status {
			case protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusAddressed:
				addressed++
			case protocol.ArtifactRevisionProposalResultsCommentResolutionsElemStatusPartiallyAddressed:
				partial++
			}
		}
		unresolved := len(compliance.UnresolvedCommentIDs)

		lines, _ := artifact.DiffLines(string(baseContent), req.Content)
		patch := artifact.UnifiedDiff(lines)

		var changeSummaryPtr *string
		if req.ChangeSummary != "" {
			changeSummaryPtr = &req.ChangeSummary
		}

		proposal := protocol.ArtifactRevisionProposal{
			Metadata: protocol.ArtifactRevisionProposalMetadata{
				Id:                proposalID,
				ReviewId:          reviewID,
				RevisionRequestId: revisionRequestID,
				Attempt:           attempt,
				Status:            protocol.ArtifactRevisionProposalMetadataStatusReady,
			},
			Base: protocol.ArtifactRevisionProposalBase{
				ArtifactId:   review.Artifact.Id,
				Version:      review.Artifact.Version,
				RevisionHash: review.Artifact.RevisionHash,
			},
			Proposed: protocol.ArtifactRevisionProposalProposed{
				Version:         proposedVersion,
				ContentHash:     artifact.Hash([]byte(req.Content)),
				ContentLocation: fmt.Sprintf(".punakawan/reviews/%s/proposals/%d.md", reviewID, attempt),
				ChangeSummary:   changeSummaryPtr,
			},
			Results: &protocol.ArtifactRevisionProposalResults{
				AddressedComments:          &addressed,
				PartiallyAddressedComments: &partial,
				UnresolvedComments:         &unresolved,
				ValidationStatus:           &validationStatus,
				CommentResolutions:         req.CommentResolutions,
			},
		}
		if err := reviews.PutProposal(proposal, []byte(req.Content), []byte(patch)); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		review.Metadata.Status = protocol.ArtifactReviewMetadataStatusProposalReady
		now := time.Now().UTC()
		review.Metadata.UpdatedAt = &now
		if err := reviews.PutReview(review); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusCreated, createProposalResponse{Proposal: proposal, Structural: structural, Compliance: compliance, Consistency: consistency})
	}
}

func parseAttempt(r *http.Request) (int, error) {
	return strconv.Atoi(r.PathValue("proposalId"))
}

// ListProposalsHandler serves GET /api/v1/reviews/{reviewId}/proposals:
// every attempt stored for the review, in order. Attempts are numbered
// densely starting at 1 (nextAttempt never skips a number), so reading
// until the first miss is a correct, if linear, way to enumerate them
// without a separate index file to keep in sync.
func ListProposalsHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		var items []protocol.ArtifactRevisionProposal
		for attempt := 1; ; attempt++ {
			_, proposal, err := reviews.GetProposal(reviewID, attempt)
			if err != nil {
				break
			}
			items = append(items, proposal)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// ProposalHandler serves
// GET /api/v1/reviews/{reviewId}/proposals/{proposalId}.
func ProposalHandler(reviews *artifact.ReviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reviewID := r.PathValue("reviewId")
		attempt, err := parseAttempt(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("api: proposalId must be an attempt number"))
			return
		}
		_, proposal, err := reviews.GetProposal(reviewID, attempt)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, proposal)
	}
}

type diffResponse struct {
	Lines   []artifact.DiffLine  `json:"lines"`
	Summary artifact.DiffSummary `json:"summary"`
}

// ProposalDiffHandler serves
// GET /api/v1/reviews/{reviewId}/proposals/{proposalId}/diff. stores
// dispatches the owning review's Artifact.Type to the matching
// artifact.Store.
func ProposalDiffHandler(reviews *artifact.ReviewStore, stores ArtifactStores) http.HandlerFunc {
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
		baseContent, _, err := store.Version(proposal.Base.ArtifactId, proposal.Base.Version)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		lines, summary := artifact.DiffLines(string(baseContent), string(content))
		writeJSON(w, http.StatusOK, diffResponse{Lines: lines, Summary: summary})
	}
}

type validationResponse struct {
	Structural validation.StructuralReport       `json:"structural"`
	Compliance validation.ReviewComplianceReport `json:"compliance"`
}

// ProposalValidationHandler serves
// GET /api/v1/reviews/{reviewId}/proposals/{proposalId}/validation. Both
// reports are recomputed live rather than read from the stored proposal
// - only the coarse passed/failed ValidationStatus is persisted, so
// re-deriving the detailed issue list from the same deterministic inputs
// is simpler than inventing a second on-disk shape for it.
func ProposalValidationHandler(reviews *artifact.ReviewStore, stores ArtifactStores) http.HandlerFunc {
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
		baseContent, _, err := store.Version(proposal.Base.ArtifactId, proposal.Base.Version)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		structural := validation.ValidateStructure(string(baseContent), string(content), proposal.Base.Version, proposal.Proposed.Version)

		comments, err := reviews.LatestComments(reviewID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var resolutions []protocol.ArtifactRevisionProposalResultsCommentResolutionsElem
		if proposal.Results != nil {
			resolutions = proposal.Results.CommentResolutions
		}
		compliance := validation.ValidateReviewCompliance(comments, resolutions)

		writeJSON(w, http.StatusOK, validationResponse{Structural: structural, Compliance: compliance})
	}
}

// AcceptProposalHandler serves
// POST /api/v1/reviews/{reviewId}/proposals/{proposalId}/accept. stores
// dispatches review.Artifact.Type to the matching artifact.Store, whose
// CreateVersion becomes the acceptance handler §4 requires each artifact
// type to provide.
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

		// Serialize this whole read-compare-write sequence per artifact
		// id: §12's conflict check ("current canonical hash == proposal
		// base hash") and the version it creates on success must be
		// atomic together, or two concurrent accepts against two
		// different reviews' proposals - both based on the same now-
		// current version - can each read Current() before either has
		// written the next version, both see current==base, and both
		// succeed. That silently accepts two proposals onto the same
		// base, which is exactly the "never silently overwrite the newer
		// version" outcome §12 forbids - only one of two concurrent
		// accepts against the same base may win; the other must observe
		// the winner's new version and conflict. store.LockArtifact
		// (not a package-level lock) so plan and recipe artifact ids
		// serialize independently of each other.
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
// POST /api/v1/reviews/{reviewId}/proposals/{proposalId}/reject. Rejection
// never touches canonical artifact state - it only updates the review's
// own status, per §16's "rejection closes the attempt without changing
// the canonical artifact."
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

type requestChangesRequest struct {
	Instruction string `json:"instruction,omitempty"`
}

// RequestChangesHandler serves
// POST /api/v1/reviews/{reviewId}/proposals/{proposalId}/request-changes:
// §16's "Request changes creates another attempt task under the same
// parent" - dispatched under a new, still-deterministic id derived from
// the next attempt number, so retrying this same call is idempotent too.
func RequestChangesHandler(reviews *artifact.ReviewStore, dispatcher revision.Dispatcher) http.HandlerFunc {
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

		var req requestChangesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		baseRequestID, err := currentRevisionRequestID(reviews, review)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		nextRequestID := fmt.Sprintf("%s-attempt-%d", baseRequestID, attempt+1)

		instruction := derefOr(review.Review.Instruction, "")
		if req.Instruction != "" {
			instruction = instruction + "\n\nAdditional guidance from request-changes: " + req.Instruction
		}

		ref, err := dispatcher.Dispatch(r.Context(), revision.Request{
			RequestID:         nextRequestID,
			ReviewID:          reviewID,
			ArtifactType:      string(review.Artifact.Type),
			ArtifactID:        review.Artifact.Id,
			BaseVersion:       review.Artifact.Version,
			BaseRevisionHash:  review.Artifact.RevisionHash,
			ReviewTitle:       review.Review.Title,
			ReviewInstruction: instruction,
			CommentCount:      0,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		review.Metadata.Status = protocol.ArtifactReviewMetadataStatusRevisionRequested
		now := time.Now().UTC()
		review.Metadata.UpdatedAt = &now
		if err := reviews.PutReview(review); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"review": review,
			"run":    runReferenceResponse{RunID: ref.RunID, ParentTaskID: ref.ParentTaskID},
		})
	}
}

// RebaseHandler serves POST /api/v1/reviews/{reviewId}/rebase: re-anchors
// a conflicted review at the artifact's current canonical version and
// returns it to draft, so the existing comment/submit flow can run again
// from a fresh base - per §12's "rebase comments onto the latest
// canonical version." It deliberately does not attempt to auto-migrate
// old comments' anchors onto the new content (that is §6's existing
// fallback-resolution chain's job the next time a comment is created
// against the refreshed content) or re-run a revision automatically -
// the user reviews the refreshed document and resubmits explicitly.
func RebaseHandler(reviews *artifact.ReviewStore, stores ArtifactStores) http.HandlerFunc {
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

		store, err := storeFor(stores, review.Artifact.Type)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		current, err := store.Current(review.Artifact.Id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		review.Artifact.Version = current.Version
		review.Artifact.RevisionHash = current.RevisionHash
		review.Metadata.Status = protocol.ArtifactReviewMetadataStatusDraft
		now := time.Now().UTC()
		review.Metadata.UpdatedAt = &now
		if err := reviews.PutReview(review); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, review)
	}
}
