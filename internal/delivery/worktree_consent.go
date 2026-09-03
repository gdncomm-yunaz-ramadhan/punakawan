package delivery

import (
	"context"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// worktreeQuestionPrefix marks the pending question asking where one
// project's delivery work should happen.
const worktreeQuestionPrefix = "worktree:"

// WorktreeQuestionReference names the pending question recorded for one
// project whose worktree mode nobody has stated yet.
func WorktreeQuestionReference(projectID string) string {
	return worktreeQuestionPrefix + projectID
}

// IsWorktreeQuestion reports whether a pending question reference is one
// of these.
func IsWorktreeQuestion(reference string) bool {
	return strings.HasPrefix(reference, worktreeQuestionPrefix)
}

// WorktreeQuestionProjectID is the project one of these references names.
func WorktreeQuestionProjectID(reference string) string {
	return strings.TrimPrefix(reference, worktreeQuestionPrefix)
}

// ProjectWorktreeMode is what this project has been told to do with its
// checkout, or empty when nobody has said.
func (s *Store) ProjectWorktreeMode(ctx context.Context, projectID string) (protocol.DeliveryProjectMetadataWorktreeMode, error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	if project.Metadata == nil || project.Metadata.WorktreeMode == nil {
		return "", nil
	}
	return *project.Metadata.WorktreeMode, nil
}

// RememberProjectWorktreeMode records the answer so no later delivery in
// this project asks again.
func (s *Store) RememberProjectWorktreeMode(ctx context.Context, idempotencyKey, projectID string, mode protocol.DeliveryProjectMetadataWorktreeMode) error {
	_, err := s.MergeProjectMetadata(ctx, idempotencyKey, projectID, protocol.DeliveryProjectMetadata{WorktreeMode: &mode})
	return err
}

// OpenWorktreeQuestion records that this delivery is waiting to be told
// where its work for projectID should happen.
//
// Punakawan will not modify somebody's working tree because a delivery
// happened to be started: a lane gets a worktree cut for it, or the
// checkout itself, and which one is a decision with two defensible
// answers rather than a default worth guessing.
func (s *Store) OpenWorktreeQuestion(ctx context.Context, idempotencyKey, orchestrationID, projectID, note string) error {
	orch, err := s.GetOrchestration(ctx, orchestrationID)
	if err != nil {
		return err
	}
	reference := WorktreeQuestionReference(projectID)
	for _, open := range orch.UnresolvedInputs {
		if open.Reference == reference {
			return nil
		}
	}
	input := protocol.DeliveryOrchestrationUnresolvedInputsElem{Reference: reference}
	if trimmed := strings.TrimSpace(note); trimmed != "" {
		input.Note = &trimmed
	}
	_, err = s.RegisterInput(ctx, idempotencyKey, orchestrationID, orch.Revision, input)
	return err
}

// WorktreeDecision is the question asked about one project, shaped as the
// options whoever answers it actually has.
func WorktreeDecision(project *protocol.DeliveryProject, lanes int) *protocol.NeedUserInput {
	where := project.RepositoryUrl
	if where == "" {
		where = project.Slug
	}
	return &protocol.NeedUserInput{
		Kind: protocol.NeedUserInputKindDecisionRequired,
		Question: fmt.Sprintf(
			"Where should this delivery's work on %s happen? It has %d lane(s), and punakawan will not touch the checkout itself unless you say so. Answer with answer_delivery_question, reference %s.",
			where, lanes, WorktreeQuestionReference(project.Id)),
		Options: []protocol.NeedUserInputOptionsElem{
			{
				Id:     string(protocol.DeliveryProjectMetadataWorktreeModeWorktree),
				Label:  "Cut a git worktree per lane",
				Impact: "Each lane gets its own directory and branch, so lanes never collide and the checkout you work in stays exactly as it is. Punakawan clones the repository first if this machine has no copy of it.",
			},
			{
				Id:     string(protocol.DeliveryProjectMetadataWorktreeModeMainCheckout),
				Label:  "Work in the checkout itself",
				Impact: "Every lane shares one working tree, and this delivery's work lands in the branch that is checked out there. Nothing is isolated, and two lanes cannot run at once.",
			},
		},
	}
}

// ProvisionLaneWorktrees creates the worktree each of projectID's lanes
// works in, and reports the ones it made.
//
// It is called once the project's mode says to - never as a side effect
// of starting a delivery. A lane that already has a worktree is left
// alone, and a lane that cannot get one is reported rather than failing
// the others: one repository being unavailable is not a reason for the
// rest of a delivery to stop.
func (s *Store) ProvisionLaneWorktrees(ctx context.Context, orchestrationID, projectID string, hints CheckoutHints) (created []string, problems []string, err error) {
	project, err := s.GetProject(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	hints.AllowClone = true
	if _, _, err := s.ResolveProjectCheckout(ctx, project, hints); err != nil {
		return nil, []string{fmt.Sprintf("project %q: %v", project.Slug, err)}, nil
	}

	lanes, err := s.ListLanes(ctx, orchestrationID)
	if err != nil {
		return nil, nil, err
	}
	for _, lane := range lanes {
		if lane.ProjectId != projectID {
			continue
		}
		if lane.WorktreePath != nil && *lane.WorktreePath != "" {
			continue
		}
		updated, err := s.CreateWorktree(ctx, NewID(), orchestrationID, lane.Id, lane.Revision)
		if err != nil {
			problems = append(problems, fmt.Sprintf("lane %s: %v", lane.Id, err))
			continue
		}
		if updated.WorktreePath != nil {
			created = append(created, *updated.WorktreePath)
		}
	}
	return created, problems, nil
}
