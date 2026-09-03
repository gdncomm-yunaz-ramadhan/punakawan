package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Gap codes. A caller matches on the code; the detail is for whoever
// reads the run later.
const (
	// GapLaneNotTerminal - a lane never reached accepted or failed, so
	// nothing ever said whether its work landed.
	GapLaneNotTerminal = "lane_not_terminal"
	// GapVerificationPending - a lane closed with verification dimensions
	// nobody reported on. Pending is not the same as passed.
	GapVerificationPending = "verification_pending"
	// GapRequirementUncovered - a requirement source was captured but no
	// lane ever covered it. The commonest shape is a Jira parent whose
	// subtasks nothing was opened for.
	GapRequirementUncovered = "requirement_uncovered"
	// GapWorkLogUnsynced - measured work never reached the provider it
	// was recorded against.
	GapWorkLogUnsynced = "worklog_unsynced"
	// GapSessionNotFinalized - a session is still open, so its usage is
	// still moving.
	GapSessionNotFinalized = "session_not_finalized"
	// GapCostUnknown - tokens were recorded but could not be priced.
	GapCostUnknown = "cost_unknown"
	// GapPlanMissing - the delivery names no plan, so nothing says what
	// it was for or what its work should have been held up against.
	GapPlanMissing = "plan_missing"
	// GapRequirementUnclear - somebody judged the requirement too vague
	// to build from and the question they asked has not been answered.
	GapRequirementUnclear = "requirement_unclear"
	// GapJiraWriteBackOff - the delivery is for a Jira issue, but this
	// workspace writes nothing back to Jira, so everything it recorded
	// stayed inside punakawan.
	GapJiraWriteBackOff = "jira_write_back_off"
)

// ReadinessGap is one reason a delivery is not finished. Subjects are the
// ids a caller needs to act on it - lane ids, worklog ids, issue keys.
type ReadinessGap struct {
	Code     string   `json:"code"`
	Detail   string   `json:"detail"`
	Subjects []string `json:"subjects,omitempty"`
}

// Readiness is the answer to "is this delivery actually finished". It is
// computed from the delivery view rather than stored, so it can never
// drift from what a reader sees.
type Readiness struct {
	Ready bool           `json:"ready"`
	Gaps  []ReadinessGap `json:"gaps,omitempty"`
}

// Summary renders the gaps as one sentence, for an error message.
func (r Readiness) Summary() string {
	if r.Ready {
		return "no gaps"
	}
	parts := make([]string, 0, len(r.Gaps))
	for _, gap := range r.Gaps {
		parts = append(parts, gap.Detail)
	}
	return strings.Join(parts, "; ")
}

// AssessCompletionReadiness reports every way view's delivery is not yet
// finished.
//
// This exists because complete_delivery used to check only the
// orchestration's revision and terminal status: a delivery whose lane was
// still runnable, whose verification was entirely pending, whose worklog
// never reached Jira, whose subtask no lane covered, and whose cost was
// unknown was accepted as complete without a word. Every one of those
// facts was already in the view - nothing was looking at them.
func AssessCompletionReadiness(view *DeliveryView) Readiness {
	if view == nil {
		return Readiness{Ready: true}
	}
	var gaps []ReadinessGap

	var openLanes, unverifiedLanes []string
	for _, lane := range view.Lanes {
		if !isLaneTerminal(lane.Status) {
			openLanes = append(openLanes, lane.LaneID)
			// A lane that never closed has pending dimensions by
			// definition; reporting both would just say the same thing
			// twice.
			continue
		}
		if lane.Verification == nil {
			continue
		}
		for _, dimension := range lane.Verification.Dimensions {
			if dimension.Status == protocol.VerificationMatrixDimensionsElemStatusPending {
				unverifiedLanes = append(unverifiedLanes, lane.LaneID)
				break
			}
		}
	}
	if len(openLanes) > 0 {
		gaps = append(gaps, ReadinessGap{
			Code:     GapLaneNotTerminal,
			Detail:   fmt.Sprintf("%d lane(s) never reached accepted or failed - close them with complete_delivery_lane", len(openLanes)),
			Subjects: openLanes,
		})
	}
	if len(unverifiedLanes) > 0 {
		gaps = append(gaps, ReadinessGap{
			Code:     GapVerificationPending,
			Detail:   fmt.Sprintf("%d closed lane(s) still have verification dimensions nobody reported on", len(unverifiedLanes)),
			Subjects: unverifiedLanes,
		})
	}

	if uncovered := uncoveredRequirementKeys(view); len(uncovered) > 0 {
		gaps = append(gaps, ReadinessGap{
			Code:     GapRequirementUncovered,
			Detail:   fmt.Sprintf("%d captured requirement(s) have no lane covering them", len(uncovered)),
			Subjects: uncovered,
		})
	}

	// Starting without a plan is a warning, not a refusal - a trivial
	// task owes nobody a document. Finishing without one is still worth
	// naming, so it is here rather than in the way.
	if view.PlanID == "" {
		gaps = append(gaps, ReadinessGap{
			Code:   GapPlanMissing,
			Detail: "this delivery names no plan, so nothing says what it was for - start_delivery for the same source with plan, or plan_id and plan_revision, attaches one",
		})
	}

	// A delivery cannot be finished against a requirement nobody has
	// explained yet. The question is already on the view; this is what
	// stops it from being completed around.
	var unclear []string
	for _, question := range view.PendingQuestions {
		if IsClarityQuestion(question) {
			unclear = append(unclear, ClarityQuestionIssueKey(question))
		}
	}
	if len(unclear) > 0 {
		gaps = append(gaps, ReadinessGap{
			Code:     GapRequirementUnclear,
			Detail:   fmt.Sprintf("%s was started as needing clarification and nobody has answered - answer_delivery_question with a clarity records the answer", strings.Join(unclear, ", ")),
			Subjects: unclear,
		})
	}

	var unsynced []string
	for _, worklog := range view.WorkLogs {
		if worklog.SyncStatus != "synced" {
			unsynced = append(unsynced, worklog.ID)
		}
	}
	if len(unsynced) > 0 {
		gaps = append(gaps, ReadinessGap{
			Code:     GapWorkLogUnsynced,
			Detail:   fmt.Sprintf("%d worklog(s) never reached the provider - retry_worklog_sync replays one without recording duplicate time", len(unsynced)),
			Subjects: unsynced,
		})
	}

	if view.Lifecycle != nil {
		var open []string
		for _, session := range view.Lifecycle.Sessions {
			if session.Status == "active" {
				open = append(open, session.ID)
			}
		}
		if len(open) > 0 {
			gaps = append(gaps, ReadinessGap{
				Code:     GapSessionNotFinalized,
				Detail:   fmt.Sprintf("%d session(s) are still open, so this delivery's usage is still moving", len(open)),
				Subjects: open,
			})
		}
	}

	if view.Telemetry.TelemetryStatus != "" && view.Telemetry.TelemetryStatus != "complete" {
		gap := ReadinessGap{
			Code:     GapCostUnknown,
			Detail:   "usage was recorded but could not be fully priced",
			Subjects: view.Telemetry.UnpricedModels,
		}
		if len(view.Telemetry.UnpricedModels) > 0 {
			gap.Detail = "no catalog price for " + strings.Join(view.Telemetry.UnpricedModels, ", ")
		}
		gaps = append(gaps, gap)
	}

	return Readiness{Ready: len(gaps) == 0, Gaps: gaps}
}

// JiraWriteBackGap reports a delivery that is for a Jira issue in a
// workspace whose Jira write-back is switched off, naming the config file
// that switches it on.
//
// It is separate from AssessCompletionReadiness because it is the only
// gap that cannot be seen in the view: the config lives in the workspace,
// not in the delivery. Without it a Jira delivery completes reporting
// unsynced worklogs and pointing at retry_worklog_sync - which redispatches
// into the same closed gate and changes nothing.
func JiraWriteBackGap(view *DeliveryView, configPath string) *ReadinessGap {
	if view == nil {
		return nil
	}
	var issues []string
	seen := map[string]bool{}
	for _, source := range view.RequirementSources {
		if source == nil || source.Provider != "jira" || source.ExternalId == nil {
			continue
		}
		key := strings.TrimSpace(*source.ExternalId)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		issues = append(issues, key)
	}
	if len(issues) == 0 {
		return nil
	}
	return &ReadinessGap{
		Code:     GapJiraWriteBackOff,
		Detail:   fmt.Sprintf("this delivery is for %s but nothing it recorded will reach Jira - no comment, worklog or transition - because Jira write-back is off in %s; punakawan setup writes that file", strings.Join(issues, ", "), configPath),
		Subjects: issues,
	}
}

// uncoveredRequirementKeys names every captured requirement source no
// lane's parent task is bound to. Reconciliation only ever mapped in one
// direction - task to source - so a source nothing named was captured,
// reported, and then silently left without any work opened for it.
func uncoveredRequirementKeys(view *DeliveryView) []string {
	if len(view.RequirementSources) == 0 {
		return nil
	}
	covered := map[string]bool{}
	if view.Lifecycle != nil {
		for _, item := range view.Lifecycle.WorkItems {
			covered[strings.ToUpper(item.JiraIssueKey)] = true
		}
	}
	// A delivery with no work-item mappings at all is a different gap
	// (its lanes are open, or it has none); calling every requirement
	// uncovered on top of that is noise, not information.
	if len(covered) == 0 {
		return nil
	}
	var out []string
	for _, source := range view.RequirementSources {
		if source == nil || source.ExternalId == nil {
			continue
		}
		key := strings.TrimSpace(*source.ExternalId)
		if key == "" || covered[strings.ToUpper(key)] {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// RecordWaivedGaps appends orchestration.completed_with_gaps naming every
// gap a caller explicitly acknowledged when completing anyway.
//
// It is a record, not a transition: it always follows the
// orchestration.completed it annotates and never changes status. Without
// it, acknowledging a gap would erase it - the delivery would read as
// cleanly complete, and the thing the check was built to make visible
// would be exactly what disappears.
func (s *Store) RecordWaivedGaps(ctx context.Context, idempotencyKey, orchestrationID string, gaps []ReadinessGap) error {
	if len(gaps) == 0 {
		return nil
	}
	payload, err := json.Marshal(map[string]any{"gaps": gaps})
	if err != nil {
		return fmt.Errorf("delivery: encode waived gaps: %w", err)
	}
	err = s.db.Write(ctx, idempotencyKey, "record waived gaps "+orchestrationID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeOrchestrationCompletedWithGaps), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return err
	}
	return nil
}
