package artifact

import (
	"errors"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func sampleReview(id string) protocol.ArtifactReview {
	return protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{
			Id:          id,
			WorkspaceId: "punakawan",
			Status:      protocol.ArtifactReviewMetadataStatusDraft,
			CreatedBy:   "user",
			CreatedAt:   time.Now().UTC(),
		},
		Artifact: protocol.ArtifactReviewArtifact{
			Type:         protocol.ArtifactReviewArtifactTypePlan,
			Id:           "plan-panel",
			Version:      3,
			RevisionHash: Hash([]byte("v3 content")),
		},
		Review: protocol.ArtifactReviewReview{
			Title: "Panel architecture revision",
		},
	}
}

func TestReviewStorePutAndGetReview(t *testing.T) {
	s := &ReviewStore{WorkspaceRoot: t.TempDir()}
	review := sampleReview("review-plan-panel-001")

	if err := s.PutReview(review); err != nil {
		t.Fatalf("PutReview: %v", err)
	}
	got, err := s.GetReview("review-plan-panel-001")
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if got.Review.Title != review.Review.Title || got.Artifact.Version != 3 {
		t.Fatalf("GetReview = %+v, want it to round-trip %+v", got, review)
	}
}

func TestReviewStorePutReviewAllowsStatusUpdates(t *testing.T) {
	s := &ReviewStore{WorkspaceRoot: t.TempDir()}
	review := sampleReview("review-plan-panel-001")
	if err := s.PutReview(review); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	review.Metadata.Status = protocol.ArtifactReviewMetadataStatusSubmitted
	if err := s.PutReview(review); err != nil {
		t.Fatalf("PutReview (update): %v", err)
	}

	got, err := s.GetReview("review-plan-panel-001")
	if err != nil {
		t.Fatalf("GetReview: %v", err)
	}
	if got.Metadata.Status != protocol.ArtifactReviewMetadataStatusSubmitted {
		t.Fatalf("Status = %q, want submitted after update", got.Metadata.Status)
	}
}

func TestReviewStoreGetReviewNotFound(t *testing.T) {
	s := &ReviewStore{WorkspaceRoot: t.TempDir()}
	if _, err := s.GetReview("no-such-review"); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("err = %v, want ErrReviewNotFound", err)
	}
}

func TestReviewStoreAppendAndLatestComments(t *testing.T) {
	s := &ReviewStore{WorkspaceRoot: t.TempDir()}
	reviewID := "review-plan-panel-001"

	c := protocol.ArtifactComment{
		Id:       "comment-003",
		ReviewId: reviewID,
		Author:   "user",
		Status:   protocol.ArtifactCommentStatusOpen,
		Anchor: protocol.ArtifactCommentAnchor{
			Kind:             protocol.ArtifactCommentAnchorKindMarkdownBlock,
			BlockId:          strp("panel.security.network-boundary"),
			BaseRevisionHash: Hash([]byte("v3 content")),
		},
		Body: "Add an optional authenticated LAN mode.",
	}
	if err := s.AppendComment(reviewID, c); err != nil {
		t.Fatalf("AppendComment: %v", err)
	}

	c.Status = protocol.ArtifactCommentStatusAddressed
	if err := s.AppendComment(reviewID, c); err != nil {
		t.Fatalf("AppendComment (status update): %v", err)
	}

	all, err := s.Comments(reviewID)
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("Comments returned %d entries, want the full 2-entry history", len(all))
	}

	latest, err := s.LatestComments(reviewID)
	if err != nil {
		t.Fatalf("LatestComments: %v", err)
	}
	if len(latest) != 1 {
		t.Fatalf("LatestComments returned %d entries, want 1 folded entry", len(latest))
	}
	if latest[0].Status != protocol.ArtifactCommentStatusAddressed {
		t.Fatalf("LatestComments[0].Status = %q, want addressed (the most recent entry)", latest[0].Status)
	}
}

func TestReviewStoreCommentsEmptyWhenNoneAppended(t *testing.T) {
	s := &ReviewStore{WorkspaceRoot: t.TempDir()}
	comments, err := s.Comments("review-plan-panel-001")
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("Comments = %v, want empty", comments)
	}
}

func sampleRevisionRequest(reviewID, requestID string) protocol.ArtifactRevisionRequest {
	return protocol.ArtifactRevisionRequest{
		Metadata: protocol.ArtifactRevisionRequestMetadata{
			Id:          requestID,
			ReviewId:    reviewID,
			SubmittedAt: time.Now().UTC(),
			SubmittedBy: "user",
		},
		BaseArtifact: protocol.ArtifactRevisionRequestBaseArtifact{
			Type:         protocol.ArtifactRevisionRequestBaseArtifactTypePlan,
			Id:           "plan-panel",
			Version:      3,
			RevisionHash: Hash([]byte("v3 content")),
		},
		Workflow: protocol.ArtifactRevisionRequestWorkflow{
			Type: protocol.ArtifactRevisionRequestWorkflowTypeRevisePlanFromReview,
		},
		Comments: protocol.ArtifactRevisionRequestComments{
			SnapshotHash: Hash([]byte("comment snapshot")),
			Count:        4,
		},
	}
}

func TestReviewStorePutAndGetRevisionRequest(t *testing.T) {
	s := &ReviewStore{WorkspaceRoot: t.TempDir()}
	req := sampleRevisionRequest("review-plan-panel-001", "revision-request-019")

	if err := s.PutRevisionRequest(req); err != nil {
		t.Fatalf("PutRevisionRequest: %v", err)
	}
	got, err := s.GetRevisionRequest("review-plan-panel-001", "revision-request-019")
	if err != nil {
		t.Fatalf("GetRevisionRequest: %v", err)
	}
	if got.Comments.Count != 4 || got.Workflow.Type != protocol.ArtifactRevisionRequestWorkflowTypeRevisePlanFromReview {
		t.Fatalf("GetRevisionRequest = %+v, want it to round-trip %+v", got, req)
	}
}

func TestReviewStorePutRevisionRequestRefusesToReplaceAnExistingSubmission(t *testing.T) {
	s := &ReviewStore{WorkspaceRoot: t.TempDir()}
	req := sampleRevisionRequest("review-plan-panel-001", "revision-request-019")
	if err := s.PutRevisionRequest(req); err != nil {
		t.Fatalf("PutRevisionRequest: %v", err)
	}
	if err := s.PutRevisionRequest(req); !errors.Is(err, ErrRevisionRequestExists) {
		t.Fatalf("err = %v, want ErrRevisionRequestExists", err)
	}
}

func sampleProposal(reviewID string, attempt int) protocol.ArtifactRevisionProposal {
	return protocol.ArtifactRevisionProposal{
		Metadata: protocol.ArtifactRevisionProposalMetadata{
			Id:                "proposal-019-01",
			ReviewId:          reviewID,
			RevisionRequestId: "revision-request-019",
			Attempt:           attempt,
			Status:            protocol.ArtifactRevisionProposalMetadataStatusReady,
		},
		Base: protocol.ArtifactRevisionProposalBase{
			ArtifactId:   "plan-panel",
			Version:      3,
			RevisionHash: Hash([]byte("v3 content")),
		},
		Proposed: protocol.ArtifactRevisionProposalProposed{
			Version:         4,
			ContentHash:     Hash([]byte("v4 content")),
			ContentLocation: ".punakawan/reviews/review-plan-panel-001/proposals/1.md",
		},
	}
}

func TestReviewStorePutAndGetProposal(t *testing.T) {
	s := &ReviewStore{WorkspaceRoot: t.TempDir()}
	proposal := sampleProposal("review-plan-panel-001", 1)

	if err := s.PutProposal(proposal, []byte("# v4 content"), []byte("--- patch ---")); err != nil {
		t.Fatalf("PutProposal: %v", err)
	}

	content, got, err := s.GetProposal("review-plan-panel-001", 1)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if string(content) != "# v4 content" {
		t.Fatalf("content = %q, want %q", content, "# v4 content")
	}
	if got.Proposed.Version != 4 {
		t.Fatalf("Proposed.Version = %d, want 4", got.Proposed.Version)
	}
}

func TestReviewStorePutProposalRefusesToReplaceAnExistingAttempt(t *testing.T) {
	s := &ReviewStore{WorkspaceRoot: t.TempDir()}
	proposal := sampleProposal("review-plan-panel-001", 1)
	if err := s.PutProposal(proposal, []byte("# v4 content"), []byte("--- patch ---")); err != nil {
		t.Fatalf("PutProposal: %v", err)
	}
	if err := s.PutProposal(proposal, []byte("# different content"), []byte("--- patch 2 ---")); !errors.Is(err, ErrProposalAttemptExists) {
		t.Fatalf("err = %v, want ErrProposalAttemptExists", err)
	}
}
