package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ProposeProjectLearningInput is propose_project_learning's input
// (agent-context plan §6.2/§6.3). The candidate is the proposed canonical
// content for the target artifact; it becomes a reviewed proposal, never a
// direct canonical write.
type ProposeProjectLearningInput struct {
	ArtifactType string         `json:"artifact_type" jsonschema:"one of workflow|project_metadata|knowledge"`
	TargetId     string         `json:"target_id" jsonschema:"the workflow id, metadata key, or knowledge record id being improved"`
	Candidate    map[string]any `json:"candidate" jsonschema:"proposed canonical content for the target artifact"`
	Rationale    string         `json:"rationale,omitempty"`
	EvidenceIds  []string       `json:"evidence_ids,omitempty"`
	SourceRunIds []string       `json:"source_run_ids,omitempty"`
	Subject      string         `json:"subject,omitempty" jsonschema:"knowledge fingerprint subject; defaults to target_id"`
	Title        string         `json:"title,omitempty"`
}

// ProposeProjectLearningOutput reports the resulting (or deduplicated)
// proposal. A dedup hit updates one pending proposal and increments its
// support count instead of opening a second one (plan §6.4).
type ProposeProjectLearningOutput struct {
	ProposalId   string `json:"proposal_id"`
	ReviewId     string `json:"review_id,omitempty"`
	Fingerprint  string `json:"fingerprint"`
	SupportCount int    `json:"support_count"`
	Deduplicated bool   `json:"deduplicated"`
	Status       string `json:"status"`
}

func proposeProjectLearningHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ProposeProjectLearningInput) (*mcp.CallToolResult, ProposeProjectLearningOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ProposeProjectLearningInput) (*mcp.CallToolResult, ProposeProjectLearningOutput, error) {
		if in.ArtifactType != learning.TypeWorkflow && in.ArtifactType != learning.TypeMetadata && in.ArtifactType != learning.TypeKnowledge {
			return nil, ProposeProjectLearningOutput{}, fmt.Errorf("propose_project_learning: artifact_type must be one of workflow|project_metadata|knowledge")
		}
		if in.TargetId == "" {
			return nil, ProposeProjectLearningOutput{}, fmt.Errorf("propose_project_learning: target_id is required")
		}

		adapter, err := learningAdapterFor(a, in.ArtifactType)
		if err != nil {
			return nil, ProposeProjectLearningOutput{}, err
		}
		candidateBytes, err := json.MarshalIndent(in.Candidate, "", "  ")
		if err != nil {
			return nil, ProposeProjectLearningOutput{}, fmt.Errorf("propose_project_learning: marshal candidate: %w", err)
		}

		fp, err := learningFingerprint(a.Workspace.ID, in, candidateBytes)
		if err != nil {
			return nil, ProposeProjectLearningOutput{}, err
		}

		store, err := learning.Open(a.Workspace.Root)
		if err != nil {
			return nil, ProposeProjectLearningOutput{}, err
		}
		now := time.Now().UTC()

		// Dedup: an equivalent pending proposal absorbs the new evidence/run
		// references and bumps support_count instead of opening a duplicate.
		if existing, ok, err := store.FindPendingByFingerprint(fp); err != nil {
			return nil, ProposeProjectLearningOutput{}, err
		} else if ok {
			existing.EvidenceIds = mergeUnique(existing.EvidenceIds, in.EvidenceIds)
			existing.SourceRunIds = mergeUnique(existing.SourceRunIds, in.SourceRunIds)
			existing.SupportCount++
			existing.UpdatedAt = now
			if err := store.Append(existing); err != nil {
				return nil, ProposeProjectLearningOutput{}, err
			}
			return nil, ProposeProjectLearningOutput{ProposalId: existing.Id, ReviewId: existing.ReviewId, Fingerprint: fp, SupportCount: existing.SupportCount, Deduplicated: true, Status: existing.Status}, nil
		}

		// New proposal: open a review + first proposal against the target
		// artifact's current revision, exactly like the panel's proposal
		// creation path, so the existing accept/reject/apply flow drives it.
		reviews := &artifact.ReviewStore{WorkspaceRoot: a.Workspace.Root}
		baseRef, err := adapter.Current(in.TargetId)
		if err != nil {
			return nil, ProposeProjectLearningOutput{}, fmt.Errorf("propose_project_learning: target %q not found: %w; %s", in.TargetId, err, createPathHint(in.ArtifactType))
		}
		baseContent, _, err := adapter.Version(in.TargetId, baseRef.Version)
		if err != nil {
			return nil, ProposeProjectLearningOutput{}, err
		}

		reviewID := randomLocalID("review")
		title := in.Title
		if title == "" {
			title = fmt.Sprintf("Learning: %s %s", in.ArtifactType, in.TargetId)
		}
		instruction := in.Rationale
		review := protocol.ArtifactReview{
			Artifact: protocol.ArtifactReviewArtifact{
				Id:           in.TargetId,
				RevisionHash: baseRef.RevisionHash,
				Type:         protocol.ArtifactReviewArtifactType(in.ArtifactType),
				Version:      baseRef.Version,
			},
			Metadata: protocol.ArtifactReviewMetadata{
				Id:          reviewID,
				Status:      protocol.ArtifactReviewMetadataStatusProposalReady,
				CreatedBy:   "agent",
				WorkspaceId: a.Workspace.ID,
				CreatedAt:   now,
			},
			Review: protocol.ArtifactReviewReview{Title: title, Instruction: &instruction},
		}
		if err := reviews.PutReview(review); err != nil {
			return nil, ProposeProjectLearningOutput{}, err
		}

		lines, _ := artifact.DiffLines(string(baseContent), string(candidateBytes))
		patch := artifact.UnifiedDiff(lines)
		validationPassed := protocol.ArtifactRevisionProposalResultsValidationStatusPassed
		var changeSummary *string
		if in.Rationale != "" {
			changeSummary = &instruction
		}
		proposal := protocol.ArtifactRevisionProposal{
			Metadata: protocol.ArtifactRevisionProposalMetadata{
				Id:                randomLocalID("proposal"),
				ReviewId:          reviewID,
				RevisionRequestId: randomLocalID("req"),
				Attempt:           1,
				Status:            protocol.ArtifactRevisionProposalMetadataStatusReady,
			},
			Base: protocol.ArtifactRevisionProposalBase{
				ArtifactId:   in.TargetId,
				Version:      baseRef.Version,
				RevisionHash: baseRef.RevisionHash,
			},
			Proposed: protocol.ArtifactRevisionProposalProposed{
				Version:         baseRef.Version + 1,
				ContentHash:     artifact.Hash(candidateBytes),
				ContentLocation: fmt.Sprintf(".punakawan/reviews/%s/proposals/1.md", reviewID),
				ChangeSummary:   changeSummary,
			},
			Results: &protocol.ArtifactRevisionProposalResults{ValidationStatus: &validationPassed},
		}
		if err := reviews.PutProposal(proposal, candidateBytes, []byte(patch)); err != nil {
			return nil, ProposeProjectLearningOutput{}, err
		}

		lp := learning.Proposal{
			Id:           randomLocalID("learn"),
			ArtifactType: in.ArtifactType,
			TargetId:     in.TargetId,
			Fingerprint:  fp,
			Rationale:    in.Rationale,
			EvidenceIds:  in.EvidenceIds,
			SourceRunIds: in.SourceRunIds,
			SupportCount: 1,
			ReviewId:     reviewID,
			Status:       learning.StatusPending,
			CreatedBy:    "agent",
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := store.Append(lp); err != nil {
			return nil, ProposeProjectLearningOutput{}, err
		}
		return nil, ProposeProjectLearningOutput{ProposalId: lp.Id, ReviewId: reviewID, Fingerprint: fp, SupportCount: 1, Deduplicated: false, Status: lp.Status}, nil
	}
}

// createPathHint tells a caller who passed a non-existent target_id where the
// create-from-scratch path lives, since propose_project_learning only ever
// proposes an improvement to an artifact that already exists - it is never the
// tool that mints a brand-new one (that confusion was reported against the
// knowledge pillar specifically: bd punokawan-h5by).
func createPathHint(artifactType string) string {
	switch artifactType {
	case learning.TypeKnowledge:
		return "propose_project_learning only improves an existing knowledge record - to create a new one from scratch use create_knowledge_record (or a dedicated dossier/role/recipe tool for structured records), then improve that record's id here"
	case learning.TypeWorkflow:
		return "propose_project_learning only improves an existing workflow definition - to create a new one use save_workflow_definition, then propose improvements against its id here"
	case learning.TypeMetadata:
		return "propose_project_learning only improves an existing metadata entry - to create one use set_project_metadata, then propose improvements against its key here"
	default:
		return "propose_project_learning only improves an artifact that already exists; create it with its dedicated tool first"
	}
}

// learningAdapterFor builds the typed artifact.Store adapter for one learning
// pillar, mirroring the panel's resolveArtifactType so the MCP propose path and
// the HTTP accept path operate on the same canonical stores.
func learningAdapterFor(a *app.App, artifactType string) (artifact.Store, error) {
	switch artifactType {
	case learning.TypeWorkflow:
		return &learning.WorkflowAdapter{Root: a.Workspace.Root}, nil
	case learning.TypeMetadata:
		return &learning.MetadataAdapter{Root: a.Workspace.Root}, nil
	case learning.TypeKnowledge:
		ks, err := a.OpenKnowledge()
		if err != nil {
			return nil, fmt.Errorf("propose_project_learning: open knowledge store: %w", err)
		}
		return &learning.KnowledgeAdapter{Store: ks}, nil
	default:
		return nil, fmt.Errorf("propose_project_learning: unknown artifact_type %q", artifactType)
	}
}

// learningFingerprint computes the deterministic dedup fingerprint for the
// proposal per pillar (plan §6.4).
func learningFingerprint(projectID string, in ProposeProjectLearningInput, candidateBytes []byte) (string, error) {
	switch in.ArtifactType {
	case learning.TypeWorkflow:
		var def workflowdef.Definition
		if err := json.Unmarshal(candidateBytes, &def); err != nil {
			return "", fmt.Errorf("propose_project_learning: workflow candidate is not a valid definition: %w", err)
		}
		graph := make([]string, 0, len(def.Steps))
		for _, s := range def.Steps {
			graph = append(graph, s.Capability+":"+s.Intent)
		}
		return learning.WorkflowFingerprint(projectID, graph), nil
	case learning.TypeMetadata:
		return learning.MetadataFingerprint(projectID, in.TargetId), nil
	case learning.TypeKnowledge:
		var rec protocol.KnowledgeRecord
		_ = json.Unmarshal(candidateBytes, &rec)
		subject := in.Subject
		if subject == "" {
			subject = in.TargetId
		}
		return learning.KnowledgeFingerprint(projectID, string(rec.Type), subject, artifact.Hash(candidateBytes)), nil
	default:
		return "", fmt.Errorf("propose_project_learning: unknown artifact_type %q", in.ArtifactType)
	}
}

func mergeUnique(existing, add []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		seen[e] = true
	}
	for _, x := range add {
		if x != "" && !seen[x] {
			existing = append(existing, x)
			seen[x] = true
		}
	}
	return existing
}
