package artifact

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ErrReviewNotFound is returned by GetReview when reviewID has no
// review.yaml yet.
var ErrReviewNotFound = errors.New("artifact: review not found")

// ErrRevisionRequestExists is returned by PutRevisionRequest when the
// given request id already has a stored submission - §5.3's "submission
// creates an immutable snapshot" means a request is written once, never
// replaced.
var ErrRevisionRequestExists = errors.New("artifact: revision request already exists")

// ErrProposalAttemptExists is returned by PutProposal when the given
// attempt number already has stored content - an attempt's proposed
// content/patch is a fixed historical record once written, even though
// its metadata (status, validation results) may still be looked up
// fresh by re-reading the same .yaml sidecar the caller last wrote.
var ErrProposalAttemptExists = errors.New("artifact: proposal attempt already exists")

// ReviewStore implements §7's review storage layout under
// <WorkspaceRoot>/.punakawan/reviews/<review-id>/: review.yaml,
// comments.jsonl, submissions/<request-id>.yaml, and
// proposals/<attempt>.{md,patch,yaml}.
type ReviewStore struct {
	WorkspaceRoot string
}

func (s *ReviewStore) reviewDir(reviewID string) string {
	return filepath.Join(s.WorkspaceRoot, ".punakawan", "reviews", reviewID)
}

func (s *ReviewStore) reviewYAMLPath(reviewID string) string {
	return filepath.Join(s.reviewDir(reviewID), "review.yaml")
}

func (s *ReviewStore) commentsPath(reviewID string) string {
	return filepath.Join(s.reviewDir(reviewID), "comments.jsonl")
}

func (s *ReviewStore) submissionPath(reviewID, requestID string) string {
	return filepath.Join(s.reviewDir(reviewID), "submissions", requestID+".yaml")
}

func (s *ReviewStore) proposalPath(reviewID string, attempt int, ext string) string {
	return filepath.Join(s.reviewDir(reviewID), "proposals", fmt.Sprintf("%d.%s", attempt, ext))
}

// PutReview writes (creating or replacing) reviewID's review.yaml.
// Unlike a revision request or a plan version, a review's own record is
// not immutable - its metadata.status legitimately transitions across
// its lifecycle (§5.1's draft/submitted/.../accepted states).
func (s *ReviewStore) PutReview(review protocol.ArtifactReview) error {
	if err := os.MkdirAll(s.reviewDir(review.Metadata.Id), 0o755); err != nil {
		return fmt.Errorf("artifact: create review dir: %w", err)
	}
	data, err := yaml.Marshal(reviewDocument{APIVersion: "punakawan.dev/v1", Kind: "ArtifactReview", Metadata: review.Metadata, Artifact: review.Artifact, Review: review.Review})
	if err != nil {
		return fmt.Errorf("artifact: marshal review.yaml: %w", err)
	}
	if err := os.WriteFile(s.reviewYAMLPath(review.Metadata.Id), data, 0o644); err != nil {
		return fmt.Errorf("artifact: write review.yaml: %w", err)
	}
	return nil
}

type reviewDocument struct {
	APIVersion string                          `yaml:"api_version"`
	Kind       string                          `yaml:"kind"`
	Metadata   protocol.ArtifactReviewMetadata `yaml:"metadata"`
	Artifact   protocol.ArtifactReviewArtifact `yaml:"artifact"`
	Review     protocol.ArtifactReviewReview   `yaml:"review"`
}

// GetReview reads reviewID's review.yaml.
func (s *ReviewStore) GetReview(reviewID string) (protocol.ArtifactReview, error) {
	data, err := os.ReadFile(s.reviewYAMLPath(reviewID))
	if os.IsNotExist(err) {
		return protocol.ArtifactReview{}, ErrReviewNotFound
	}
	if err != nil {
		return protocol.ArtifactReview{}, fmt.Errorf("artifact: read review.yaml: %w", err)
	}
	var doc reviewDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return protocol.ArtifactReview{}, fmt.Errorf("artifact: parse review.yaml: %w", err)
	}
	return protocol.ArtifactReview{Metadata: doc.Metadata, Artifact: doc.Artifact, Review: doc.Review}, nil
}

// AppendComment appends one comment record to reviewID's comments.jsonl.
// A later append with the same comment ID (e.g. a status transition
// open -> addressed) is a new history entry, not an edit - Comments
// returns the full history in append order; LatestComments folds it
// down to each ID's most recent entry.
func (s *ReviewStore) AppendComment(reviewID string, c protocol.ArtifactComment) error {
	if err := os.MkdirAll(s.reviewDir(reviewID), 0o755); err != nil {
		return fmt.Errorf("artifact: create review dir: %w", err)
	}
	f, err := os.OpenFile(s.commentsPath(reviewID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("artifact: open comments.jsonl: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(c); err != nil {
		return fmt.Errorf("artifact: encode comment %s: %w", c.Id, err)
	}
	return nil
}

// Comments returns every comment record for reviewID, in append order.
func (s *ReviewStore) Comments(reviewID string) ([]protocol.ArtifactComment, error) {
	f, err := os.Open(s.commentsPath(reviewID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("artifact: open comments.jsonl: %w", err)
	}
	defer f.Close()

	var out []protocol.ArtifactComment
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var c protocol.ArtifactComment
		if err := json.Unmarshal(line, &c); err != nil {
			return nil, fmt.Errorf("artifact: decode comment: %w", err)
		}
		out = append(out, c)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("artifact: scan comments.jsonl: %w", err)
	}
	return out, nil
}

// LatestComments folds Comments' full history down to each comment ID's
// most recent entry.
func (s *ReviewStore) LatestComments(reviewID string) ([]protocol.ArtifactComment, error) {
	all, err := s.Comments(reviewID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]protocol.ArtifactComment, len(all))
	order := make([]string, 0, len(all))
	for _, c := range all {
		if _, seen := byID[c.Id]; !seen {
			order = append(order, c.Id)
		}
		byID[c.Id] = c
	}
	out := make([]protocol.ArtifactComment, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, nil
}

// PutRevisionRequest writes req's immutable submission snapshot.
func (s *ReviewStore) PutRevisionRequest(req protocol.ArtifactRevisionRequest) error {
	dir := filepath.Join(s.reviewDir(req.Metadata.ReviewId), "submissions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("artifact: create submissions dir: %w", err)
	}
	path := s.submissionPath(req.Metadata.ReviewId, req.Metadata.Id)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%w: %s", ErrRevisionRequestExists, req.Metadata.Id)
	}
	data, err := yaml.Marshal(revisionRequestDocument{APIVersion: "punakawan.dev/v1", Kind: "ArtifactRevisionRequest", Metadata: req.Metadata, BaseArtifact: req.BaseArtifact, Workflow: req.Workflow, Comments: req.Comments})
	if err != nil {
		return fmt.Errorf("artifact: marshal revision request: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("artifact: write revision request: %w", err)
	}
	return nil
}

type revisionRequestDocument struct {
	APIVersion   string                                       `yaml:"api_version"`
	Kind         string                                       `yaml:"kind"`
	Metadata     protocol.ArtifactRevisionRequestMetadata     `yaml:"metadata"`
	BaseArtifact protocol.ArtifactRevisionRequestBaseArtifact `yaml:"base_artifact"`
	Workflow     protocol.ArtifactRevisionRequestWorkflow     `yaml:"workflow"`
	Comments     protocol.ArtifactRevisionRequestComments     `yaml:"comments"`
}

// GetRevisionRequest reads reviewID's requestID submission.
func (s *ReviewStore) GetRevisionRequest(reviewID, requestID string) (protocol.ArtifactRevisionRequest, error) {
	data, err := os.ReadFile(s.submissionPath(reviewID, requestID))
	if err != nil {
		return protocol.ArtifactRevisionRequest{}, fmt.Errorf("artifact: read revision request: %w", err)
	}
	var doc revisionRequestDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return protocol.ArtifactRevisionRequest{}, fmt.Errorf("artifact: parse revision request: %w", err)
	}
	return protocol.ArtifactRevisionRequest{Metadata: doc.Metadata, BaseArtifact: doc.BaseArtifact, Workflow: doc.Workflow, Comments: doc.Comments}, nil
}

// PutProposal writes one revision attempt's content, machine patch, and
// metadata. The attempt's content/patch may only be written once -
// content_hash/content_location are a fixed historical record, even
// though the caller may still legitimately re-marshal the .yaml sidecar
// later to update results.validation_status once async validation
// completes (PutProposal always rewrites the .yaml; only content/patch
// are write-once).
func (s *ReviewStore) PutProposal(proposal protocol.ArtifactRevisionProposal, content, patch []byte) error {
	dir := filepath.Join(s.reviewDir(proposal.Metadata.ReviewId), "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("artifact: create proposals dir: %w", err)
	}

	contentPath := s.proposalPath(proposal.Metadata.ReviewId, proposal.Metadata.Attempt, "md")
	if _, err := os.Stat(contentPath); err == nil {
		return fmt.Errorf("%w: attempt %d of review %s", ErrProposalAttemptExists, proposal.Metadata.Attempt, proposal.Metadata.ReviewId)
	}
	if err := os.WriteFile(contentPath, content, 0o644); err != nil {
		return fmt.Errorf("artifact: write proposal content: %w", err)
	}
	if err := os.WriteFile(s.proposalPath(proposal.Metadata.ReviewId, proposal.Metadata.Attempt, "patch"), patch, 0o644); err != nil {
		return fmt.Errorf("artifact: write proposal patch: %w", err)
	}

	data, err := yaml.Marshal(revisionProposalDocument{APIVersion: "punakawan.dev/v1", Kind: "ArtifactRevisionProposal", Metadata: proposal.Metadata, Base: proposal.Base, Proposed: proposal.Proposed, Results: proposal.Results})
	if err != nil {
		return fmt.Errorf("artifact: marshal proposal metadata: %w", err)
	}
	if err := os.WriteFile(s.proposalPath(proposal.Metadata.ReviewId, proposal.Metadata.Attempt, "yaml"), data, 0o644); err != nil {
		return fmt.Errorf("artifact: write proposal metadata: %w", err)
	}
	return nil
}

type revisionProposalDocument struct {
	APIVersion string                                    `yaml:"api_version"`
	Kind       string                                    `yaml:"kind"`
	Metadata   protocol.ArtifactRevisionProposalMetadata `yaml:"metadata"`
	Base       protocol.ArtifactRevisionProposalBase     `yaml:"base"`
	Proposed   protocol.ArtifactRevisionProposalProposed `yaml:"proposed"`
	Results    *protocol.ArtifactRevisionProposalResults `yaml:"results,omitempty"`
}

// GetProposal reads one attempt's content and metadata.
func (s *ReviewStore) GetProposal(reviewID string, attempt int) ([]byte, protocol.ArtifactRevisionProposal, error) {
	content, err := os.ReadFile(s.proposalPath(reviewID, attempt, "md"))
	if err != nil {
		return nil, protocol.ArtifactRevisionProposal{}, fmt.Errorf("artifact: read proposal content: %w", err)
	}
	data, err := os.ReadFile(s.proposalPath(reviewID, attempt, "yaml"))
	if err != nil {
		return nil, protocol.ArtifactRevisionProposal{}, fmt.Errorf("artifact: read proposal metadata: %w", err)
	}
	var doc revisionProposalDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, protocol.ArtifactRevisionProposal{}, fmt.Errorf("artifact: parse proposal metadata: %w", err)
	}
	return content, protocol.ArtifactRevisionProposal{Metadata: doc.Metadata, Base: doc.Base, Proposed: doc.Proposed, Results: doc.Results}, nil
}
