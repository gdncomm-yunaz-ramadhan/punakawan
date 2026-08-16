package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/worklogalloc"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ErrRequiredCheckFailed is returned by ApproveManifest when the
// manifest's checks snapshot recorded a failed required check: fail
// early, before implementation, rather than let a human wave through
// an approval a preflight already knows will break.
var ErrRequiredCheckFailed = errors.New("delivery: manifest has a failed required check")

// ManifestPlan is the fixed, immutable-once-created scope of an
// ApprovalManifest. Nothing after CreateApprovalManifest can widen it -
// a changed scope requires a new manifest.
type ManifestPlan struct {
	PlannedBaseRef    string
	PlannedBranches   []string
	ExpectsJiraWrites bool
	ExpectsCommits    bool
	ExpectsPushes     bool
	ExpectsPRs        bool
	Checks            []protocol.PreflightCheck

	// ProposedWorklog is the caller's already-computed worklogalloc.Allocate
	// result for this manifest's parent tasks, so a proposed worklog is
	// visible to a human before they approve project delivery.
	// CreateApprovalManifest does not compute this itself - it has no
	// access to a project's test-run evidence or
	// its configured Jira subtasks, both of which live outside this
	// package - it only persists whatever allocation the caller already
	// derived (typically via deliverysummary.Summary.VerifiedHours feeding
	// worklogalloc.Allocate). A zero-value Allocation (TotalHours == 0)
	// means no proposed worklog for this manifest, which is a normal state
	// for a project with no accumulated test-run evidence yet, not an
	// error.
	ProposedWorklog worklogalloc.Allocation
}

// CreateApprovalManifest builds one approval manifest covering
// parentTaskIDs, all of which must already be routed to the same
// projectID (a manifest never spans, and can never be reinterpreted to
// span, more than one project).
func (s *Store) CreateApprovalManifest(ctx context.Context, idempotencyKey, id, orchestrationID, projectID string, parentTaskIDs []string, plan ManifestPlan) (*protocol.ApprovalManifest, error) {
	if len(parentTaskIDs) == 0 {
		return nil, fmt.Errorf("delivery: an approval manifest requires at least one parent task id")
	}
	err := s.db.Write(ctx, idempotencyKey, "create manifest "+id, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		tasks, err := allParentTasks(orchestrationID, events)
		if err != nil {
			return err
		}
		for _, taskID := range parentTaskIDs {
			task, ok := tasks[taskID]
			if !ok {
				return ErrNotFound
			}
			if task.ProjectId == nil || *task.ProjectId != projectID {
				return ErrScopeMismatch
			}
		}

		checks := make([]map[string]interface{}, 0, len(plan.Checks))
		for _, c := range plan.Checks {
			entry := map[string]interface{}{"name": c.Name, "status": string(c.Status), "classification": string(c.Classification)}
			if c.Detail != nil {
				entry["detail"] = *c.Detail
			}
			checks = append(checks, entry)
		}
		worklogs := make([]map[string]interface{}, 0, len(plan.ProposedWorklog.Worklogs))
		for _, w := range plan.ProposedWorklog.Worklogs {
			worklogs = append(worklogs, map[string]interface{}{
				"bucket": string(w.Bucket), "subtask_key": w.SubtaskKey,
				"subtask_name": w.SubtaskName, "hours": w.Hours,
			})
		}
		payload, err := json.Marshal(map[string]interface{}{
			"project_id": projectID, "parent_task_ids": parentTaskIDs,
			"planned_base_ref": plan.PlannedBaseRef, "planned_branches": nonNil(plan.PlannedBranches),
			"expects_jira_writes": plan.ExpectsJiraWrites, "expects_commits": plan.ExpectsCommits,
			"expects_pushes": plan.ExpectsPushes, "expects_prs": plan.ExpectsPRs,
			"checks":                          checks,
			"proposed_worklog_total_hours":    plan.ProposedWorklog.TotalHours,
			"proposed_worklog":                worklogs,
			"proposed_worklog_unmapped_hours": plan.ProposedWorklog.UnmappedHours,
		})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &id, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeManifestCreated), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetApprovalManifest(ctx, orchestrationID, id)
}

// GetApprovalManifest fails closed (ErrNotFound) when manifestID does
// not exist within orchestrationID's own event log.
func (s *Store) GetApprovalManifest(ctx context.Context, orchestrationID, manifestID string) (*protocol.ApprovalManifest, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	return reduceApprovalManifest(orchestrationID, manifestID, events)
}

// ApproveManifest and RejectManifest both reject an agent role
// (semar/gareng/petruk/bagong) approving its own manifest, reusing
// internal/approvals' existing guard rather than duplicating the role
// name list. ApproveManifest additionally fails closed
// (ErrRequiredCheckFailed) if the manifest's checks snapshot recorded
// any required-classification check as failed - optional and
// delegated-to-ci checks never block approval, matching this task's
// "fail early" goal without over-blocking local work.
func (s *Store) ApproveManifest(ctx context.Context, idempotencyKey, orchestrationID, manifestID, approvedBy string) (*protocol.ApprovalManifest, error) {
	manifest, err := s.GetApprovalManifest(ctx, orchestrationID, manifestID)
	if err != nil {
		return nil, err
	}
	for _, c := range manifest.Checks {
		if c.Classification == protocol.ApprovalManifestChecksElemClassificationRequired && c.Status == protocol.ApprovalManifestChecksElemStatusFail {
			return nil, ErrRequiredCheckFailed
		}
	}
	return s.decideManifest(ctx, idempotencyKey, orchestrationID, manifestID, approvedBy, protocol.DeliveryEventTypeManifestApproved)
}

func (s *Store) RejectManifest(ctx context.Context, idempotencyKey, orchestrationID, manifestID, decidedBy string) (*protocol.ApprovalManifest, error) {
	return s.decideManifest(ctx, idempotencyKey, orchestrationID, manifestID, decidedBy, protocol.DeliveryEventTypeManifestRejected)
}

func (s *Store) decideManifest(ctx context.Context, idempotencyKey, orchestrationID, manifestID, decidedBy string, eventType protocol.DeliveryEventType) (*protocol.ApprovalManifest, error) {
	if approvals.IsAgentRoleIdentifier(decidedBy) {
		return nil, fmt.Errorf("delivery: %w: %q may not decide its own manifest", ErrInvalidState, decidedBy)
	}
	err := s.db.Write(ctx, idempotencyKey, string(eventType)+" "+manifestID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		manifest, err := reduceApprovalManifest(orchestrationID, manifestID, events)
		if err != nil {
			return err
		}
		if manifest.Status != protocol.ApprovalManifestStatusPending {
			return ErrInvalidState
		}
		payload, err := json.Marshal(map[string]interface{}{"approved_by": decidedBy})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &manifestID, IdempotencyKey: idempotencyKey,
			Type: string(eventType), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetApprovalManifest(ctx, orchestrationID, manifestID)
}
