// deliveryview.go assembles DeliveryView, the one read model the
// start-delivery facade tools share instead of each hand-rolling its own
// aggregation over lanes, tasks, edges, and manifests. It never mutates
// anything - every field is derived by
// replaying the orchestration's own event log, exactly like every
// other Get*/List* method in this package.
package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ProjectSummary rolls up one project's involvement in an
// orchestration: whether the run explicitly attached it, which of its
// lanes belong to the run, and how many sit in each scheduling status.
type ProjectSummary struct {
	ProjectID string `json:"project_id"`

	// Attached distinguishes the two ways a project can show up here. A
	// project the run explicitly attached is a statement that the run
	// involves it, and it appears even with no lanes at all. A project
	// that only shows up because some lane names it appears with
	// attached false - including a project detached after its lanes
	// finished, whose completed work the run still honestly reports.
	// Only an attached project can be detached.
	Attached bool `json:"attached"`

	LaneIDs        []string                            `json:"lane_ids"`
	CountsByStatus map[protocol.DeliveryLaneStatus]int `json:"counts_by_status"`
}

// LaneSummary is one lane's status at a glance, plus its published
// pull request fields once it has one.
type LaneSummary struct {
	LaneID       string                      `json:"lane_id"`
	ProjectID    string                      `json:"project_id"`
	ParentTaskID string                      `json:"parent_task_id,omitempty"`
	Status       protocol.DeliveryLaneStatus `json:"status"`
	BlockedBy    []string                    `json:"blocked_by,omitempty"`
	PRURL        string                      `json:"pr_url,omitempty"`
	PRNumber     int                         `json:"pr_number,omitempty"`
	PRProvider   string                      `json:"pr_provider,omitempty"`
	Repository   string                      `json:"repository,omitempty"`
	Branch       string                      `json:"branch,omitempty"`
	Commits      []string                    `json:"commits,omitempty"`

	// Worker, WorktreePath, BaseSha, and BaseRemote surface protocol.
	// DeliveryLane's lease/worktree fields, unwrapped from their pointer
	// form (empty when the lane was never leased/never got a worktree).
	Worker       string `json:"worker,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	BaseSha      string `json:"base_sha,omitempty"`
	BaseRemote   string `json:"base_remote,omitempty"`

	// SemarRecordID through BagongRecordID mark how far this attempt has
	// progressed through the fixed Semar->Gareng->Petruk->Bagong pipeline -
	// each is set once that stage's record is recorded, and empty before
	// then.
	SemarRecordID  string `json:"semar_record_id,omitempty"`
	GarengRecordID string `json:"gareng_record_id,omitempty"`
	PetrukRecordID string `json:"petruk_record_id,omitempty"`
	BagongRecordID string `json:"bagong_record_id,omitempty"`

	Attempt          int        `json:"attempt,omitempty"`
	RepairCycleCount int        `json:"repair_cycle_count,omitempty"`
	EscalatedAt      *time.Time `json:"escalated_at,omitempty"`

	// SessionID is the workflow run that opened this lane, empty when it
	// was opened without naming one. It is not the same question as
	// Worker, which is whoever holds the lane's lease right now.
	SessionID string `json:"session_id,omitempty"`

	// Evidence lists this lane's recorded artifacts by reference only -
	// never their bytes - so a caller links to (or fetches) the
	// underlying content instead of this view inlining it.
	Evidence []EvidenceRef `json:"evidence,omitempty"`

	Verification *protocol.VerificationMatrix `json:"verification,omitempty"`
	BagongReview *protocol.ReviewConclusion   `json:"bagong_review,omitempty"`
}

// EvidenceRef is one evidence artifact's linkable metadata: enough for a
// caller to fetch or display a link to the underlying content-addressed
// bytes without this view ever carrying the bytes themselves.
type EvidenceRef struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	MediaType   string `json:"media_type"`
	ByteSize    int    `json:"byte_size"`
	ContentHash string `json:"content_hash"`
}

// BlockerSummary names one lane still waiting on unresolved
// predecessor task ids.
type BlockerSummary struct {
	LaneID       string   `json:"lane_id"`
	ParentTaskID string   `json:"parent_task_id,omitempty"`
	BlockedBy    []string `json:"blocked_by"`
}

type AuditEvent struct {
	Sequence   int                        `json:"sequence"`
	Type       protocol.DeliveryEventType `json:"type"`
	EntityID   string                     `json:"entity_id,omitempty"`
	OccurredAt time.Time                  `json:"occurred_at"`
}

type JiraActivity struct {
	EventType string    `json:"event_type"`
	EntityID  string    `json:"entity_id,omitempty"`
	IssueKey  string    `json:"issue_key"`
	FiredAt   time.Time `json:"fired_at"`
}

// DeliveryView is the bounded, human-and-agent-readable snapshot of one
// orchestration: its own state, every lane grouped by project, every
// still-blocked lane, every pending approval, every pending question,
// and one honest sentence about what to do next.
type DeliveryView struct {
	Orchestration *protocol.DeliveryOrchestration `json:"orchestration"`

	// Title is the label to show instead of the orchestration's opaque
	// id. It is whatever title the run was started with, or - when it was
	// started without one, as every run predating titles was - a label
	// derived mechanically from the run's own requirement references (see
	// deriveOrchestrationTitle). Never empty, so no consumer has to
	// choose a fallback of its own.
	Title string `json:"title"`

	// Description, PlanRecordID, PlanID, PlanRevision, and SessionID
	// unwrap the orchestration's own optional fields from their pointer
	// form, the same way LaneSummary unwraps a lane's, so every consumer
	// reads one flat shape instead of some fields here and others by
	// reaching into Orchestration. Each is empty/zero when never set.
	// Unlike Title, none of them is ever derived: a run that never
	// recorded prose, a plan, or a session simply reports none.
	//
	// PlanRecordID is deprecated (§4.4): it names a knowledge record from
	// the old plan-as-knowledge write path. New deliveries should report
	// PlanID+PlanRevision instead, naming an exact internal/plan
	// revision.
	Description  string `json:"description,omitempty"`
	PlanRecordID string `json:"plan_record_id,omitempty"`
	PlanID       string `json:"plan_id,omitempty"`
	PlanRevision int    `json:"plan_revision,omitempty"`
	SessionID    string `json:"session_id,omitempty"`

	Projects         []ProjectSummary             `json:"projects"`
	Lanes            []LaneSummary                `json:"lanes"`
	Blockers         []BlockerSummary             `json:"blockers"`
	PendingApprovals []*protocol.ApprovalManifest `json:"pending_approvals"`
	PendingQuestions []string                     `json:"pending_questions"`
	NextAction       string                       `json:"next_action"`
	Timeline         []AuditEvent                 `json:"timeline"`
	JiraActivity     []JiraActivity               `json:"jira_activity"`
	WorkLogs         []WorkLogEntry               `json:"worklogs"`
	WorkLogSeconds   int                          `json:"worklog_seconds"`

	// LatestSeq is the highest event sequence number reflected in this
	// view - pass it back as a later call's SinceSeq to learn what
	// changed since. Always populated, regardless of whether SinceSeq
	// was given.
	LatestSeq int `json:"latest_seq"`

	// NewlyRunnableLaneIDs are lanes now at runnable status whose
	// dependencies were still unmet as of SinceSeq (or, when SinceSeq is
	// 0/omitted, every currently runnable lane - there is no prior
	// baseline to diff against). Only BuildDeliveryViewSince populates
	// this; BuildDeliveryView always leaves it empty.
	NewlyRunnableLaneIDs []string `json:"newly_runnable_lane_ids"`
}

// allApprovalManifests reduces every manifest entity in events into its
// current ApprovalManifest state, keyed by manifest id. reduce.go gives
// every other entity type (lanes, parent tasks, dependency edges,
// requirement sources) its own allX enumeration next to its reduceX
// function; this one lives here instead because BuildDeliveryView is
// its only caller and manifests.go itself never needed to enumerate
// "every manifest in an orchestration" before now.
func allApprovalManifests(orchestrationID string, events []protocol.DeliveryEvent) (map[string]*protocol.ApprovalManifest, error) {
	ids := map[string]bool{}
	for _, ev := range events {
		if ev.Type == protocol.DeliveryEventTypeManifestCreated && ev.EntityId != nil {
			ids[*ev.EntityId] = true
		}
	}
	out := make(map[string]*protocol.ApprovalManifest, len(ids))
	for id := range ids {
		m, err := reduceApprovalManifest(orchestrationID, id, events)
		if err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, nil
}

// BuildDeliveryView assembles orchestrationID's current DeliveryView by
// replaying its event log once and deriving every lane, parent task,
// dependency edge, and approval manifest from that single pass - the
// same enumeration approach list_runnable_lanes/report_discovered_dependency
// already use (allLanes/ListGraph), not a new one. NewlyRunnableLaneIDs is
// always left empty; use BuildDeliveryViewSince for that.
func (s *Store) BuildDeliveryView(ctx context.Context, orchestrationID string) (*DeliveryView, error) {
	return s.buildDeliveryView(ctx, orchestrationID, -1)
}

// BuildDeliveryViewSince is BuildDeliveryView, plus NewlyRunnableLaneIDs:
// lanes at runnable status now that were not as of sinceSeq (every event
// with Sequence <= sinceSeq). sinceSeq of 0 means "since the beginning" -
// every currently runnable lane, since there is no prior baseline to diff
// against - which is exactly what a caller polling for the first time
// wants. Pass the prior call's returned LatestSeq to see what's changed
// since then.
func (s *Store) BuildDeliveryViewSince(ctx context.Context, orchestrationID string, sinceSeq int) (*DeliveryView, error) {
	return s.buildDeliveryView(ctx, orchestrationID, sinceSeq)
}

// diffDisabled is buildDeliveryView's sentinel sinceSeq: BuildDeliveryView's
// existing callers never asked for a diff, and -1 can never equal a real
// event Sequence (those start at 0), so it cleanly means "skip the diff
// pass, leave NewlyRunnableLaneIDs empty" without a bool parameter.
const diffDisabled = -1

func (s *Store) buildDeliveryView(ctx context.Context, orchestrationID string, sinceSeq int) (*DeliveryView, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	orch, err := reduceOrchestration(orchestrationID, events)
	if err != nil {
		return nil, err
	}
	laneMap, err := allLanes(orchestrationID, events)
	if err != nil {
		return nil, err
	}
	manifestMap, err := allApprovalManifests(orchestrationID, events)
	if err != nil {
		return nil, err
	}
	sourceMap, err := allRequirementSources(orchestrationID, events)
	if err != nil {
		return nil, err
	}

	view := &DeliveryView{
		Orchestration:        orch,
		Title:                orchestrationTitle(orch, sortedRequirementSources(sourceMap)),
		Projects:             []ProjectSummary{},
		Lanes:                []LaneSummary{},
		Blockers:             []BlockerSummary{},
		PendingApprovals:     []*protocol.ApprovalManifest{},
		PendingQuestions:     []string{},
		Timeline:             []AuditEvent{},
		JiraActivity:         []JiraActivity{},
		WorkLogs:             []WorkLogEntry{},
		NewlyRunnableLaneIDs: []string{},
	}
	if orch.Description != nil {
		view.Description = *orch.Description
	}
	if orch.PlanRecordId != nil {
		view.PlanRecordID = *orch.PlanRecordId
	}
	if orch.PlanId != nil {
		view.PlanID = *orch.PlanId
	}
	if orch.PlanRevision != nil {
		view.PlanRevision = *orch.PlanRevision
	}
	if orch.SessionId != nil {
		view.SessionID = *orch.SessionId
	}
	for _, ev := range events {
		if ev.Sequence > view.LatestSeq {
			view.LatestSeq = ev.Sequence
		}
		audit := AuditEvent{Sequence: ev.Sequence, Type: ev.Type, OccurredAt: ev.OccurredAt}
		if ev.EntityId != nil {
			audit.EntityID = *ev.EntityId
		}
		view.Timeline = append(view.Timeline, audit)
	}
	jiraActivity, err := s.listJiraActivity(ctx, orchestrationID)
	if err != nil {
		return nil, err
	}
	view.JiraActivity = jiraActivity
	workLogs, err := s.ListWorkLogs(ctx, orchestrationID)
	if err != nil {
		return nil, err
	}
	view.WorkLogs = workLogs
	for _, workLog := range workLogs {
		view.WorkLogSeconds += workLog.DurationSeconds
	}

	laneIDs := make([]string, 0, len(laneMap))
	for id := range laneMap {
		laneIDs = append(laneIDs, id)
	}
	sort.Strings(laneIDs)

	laneIDsByProject := map[string][]string{}
	countsByProject := map[string]map[protocol.DeliveryLaneStatus]int{}

	// Evidence is fetched once for the whole orchestration, then grouped
	// by lane in memory, rather than one ListArtifacts call per lane. A
	// failed lookup degrades gracefully - evidence is supplementary, not
	// core lane state, so it is worth losing rather than failing the
	// whole view over.
	evidenceByLane := map[string][]EvidenceRef{}
	if artifacts, err := s.ListArtifacts(ctx, ArtifactFilter{OrchestrationID: orchestrationID}); err == nil {
		for _, a := range artifacts {
			if a.LaneId == nil {
				continue
			}
			evidenceByLane[*a.LaneId] = append(evidenceByLane[*a.LaneId], EvidenceRef{
				ID:          a.Id,
				Kind:        string(a.Kind),
				MediaType:   a.MediaType,
				ByteSize:    a.ByteSize,
				ContentHash: a.ContentHash,
			})
		}
	}
	commitsByLane := map[string][]string{}
	for _, event := range events {
		if event.Type == protocol.DeliveryEventTypeLaneCommitRecorded && event.EntityId != nil {
			if sha := stringField(event.Payload, "sha"); sha != "" {
				commitsByLane[*event.EntityId] = append(commitsByLane[*event.EntityId], sha)
			}
		}
	}

	for _, id := range laneIDs {
		l := laneMap[id]
		summary := LaneSummary{
			LaneID:    l.Id,
			ProjectID: l.ProjectId,
			Status:    l.Status,
			BlockedBy: l.BlockedBy,
			Evidence:  evidenceByLane[l.Id],
			Commits:   commitsByLane[l.Id],
		}
		if l.ParentTaskId != nil {
			summary.ParentTaskID = *l.ParentTaskId
		}
		if l.PrUrl != nil {
			summary.PRURL = *l.PrUrl
		}
		if l.PrNumber != nil {
			summary.PRNumber = *l.PrNumber
		}
		if l.PrProvider != nil {
			summary.PRProvider = string(*l.PrProvider)
		}
		if l.PrRepoSlug != nil {
			summary.Repository = *l.PrRepoSlug
		}
		if l.Branch != nil {
			summary.Branch = *l.Branch
		}
		if l.LeaseWorkerId != nil {
			summary.Worker = *l.LeaseWorkerId
		}
		if l.WorktreePath != nil {
			summary.WorktreePath = *l.WorktreePath
		}
		if l.BaseSha != nil {
			summary.BaseSha = *l.BaseSha
		}
		if l.BaseRemote != nil {
			summary.BaseRemote = *l.BaseRemote
		}
		if l.SemarRecordId != nil {
			summary.SemarRecordID = *l.SemarRecordId
		}
		if l.GarengRecordId != nil {
			summary.GarengRecordID = *l.GarengRecordId
		}
		if l.PetrukRecordId != nil {
			summary.PetrukRecordID = *l.PetrukRecordId
		}
		if l.BagongRecordId != nil {
			summary.BagongRecordID = *l.BagongRecordId
		}
		if l.Attempt != nil {
			summary.Attempt = *l.Attempt
		}
		if l.RepairCycleCount != nil {
			summary.RepairCycleCount = *l.RepairCycleCount
		}
		summary.EscalatedAt = l.EscalatedAt
		if l.SessionId != nil {
			summary.SessionID = *l.SessionId
		}
		verification, err := s.BuildVerificationMatrix(ctx, orchestrationID, l.Id)
		if err != nil {
			return nil, err
		}
		summary.Verification = verification
		review, err := s.GetLatestReviewConclusion(ctx, orchestrationID, l.Id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		summary.BagongReview = review
		view.Lanes = append(view.Lanes, summary)

		laneIDsByProject[l.ProjectId] = append(laneIDsByProject[l.ProjectId], l.Id)
		if countsByProject[l.ProjectId] == nil {
			countsByProject[l.ProjectId] = map[protocol.DeliveryLaneStatus]int{}
		}
		countsByProject[l.ProjectId][l.Status]++

		if len(l.BlockedBy) > 0 {
			blocker := BlockerSummary{LaneID: l.Id, BlockedBy: l.BlockedBy}
			if l.ParentTaskId != nil {
				blocker.ParentTaskID = *l.ParentTaskId
			}
			view.Blockers = append(view.Blockers, blocker)
		}
	}

	// A project belongs in this list if the run attached it or if any of
	// the run's lanes names it - the two overlap in the common case but
	// neither contains the other. An attached project with no lanes yet
	// is precisely what a caller sees between attaching it and
	// decomposing work into it, and a project detached after its lanes
	// finished still has to report that finished work rather than
	// vanishing from the run's history.
	attached := map[string]bool{}
	for _, id := range orch.ProjectIds {
		attached[id] = true
	}
	seen := map[string]bool{}
	projectIDs := make([]string, 0, len(laneIDsByProject)+len(orch.ProjectIds))
	for _, id := range orch.ProjectIds {
		if !seen[id] {
			seen[id] = true
			projectIDs = append(projectIDs, id)
		}
	}
	for id := range laneIDsByProject {
		if !seen[id] {
			seen[id] = true
			projectIDs = append(projectIDs, id)
		}
	}
	sort.Strings(projectIDs)
	for _, id := range projectIDs {
		counts := countsByProject[id]
		if counts == nil {
			counts = map[protocol.DeliveryLaneStatus]int{}
		}
		laneIDs := laneIDsByProject[id]
		if laneIDs == nil {
			laneIDs = []string{}
		}
		view.Projects = append(view.Projects, ProjectSummary{
			ProjectID:      id,
			Attached:       attached[id],
			LaneIDs:        laneIDs,
			CountsByStatus: counts,
		})
	}

	manifestIDs := make([]string, 0, len(manifestMap))
	for id := range manifestMap {
		manifestIDs = append(manifestIDs, id)
	}
	sort.Slice(manifestIDs, func(i, j int) bool {
		a, b := manifestMap[manifestIDs[i]], manifestMap[manifestIDs[j]]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.Id < b.Id
	})
	for _, id := range manifestIDs {
		m := manifestMap[id]
		if m.Status == protocol.ApprovalManifestStatusPending {
			view.PendingApprovals = append(view.PendingApprovals, m)
		}
	}

	for _, in := range orch.UnresolvedInputs {
		view.PendingQuestions = append(view.PendingQuestions, in.Reference)
	}

	view.NextAction = computeNextAction(orch, view.Lanes, view.PendingApprovals)

	if sinceSeq != diffDisabled {
		newly, err := newlyRunnableLaneIDs(orchestrationID, events, laneMap, sinceSeq)
		if err != nil {
			return nil, err
		}
		view.NewlyRunnableLaneIDs = newly
	}

	return view, nil
}

func (s *Store) listJiraActivity(ctx context.Context, orchestrationID string) ([]JiraActivity, error) {
	rows, err := s.db.Reader().QueryContext(ctx,
		`SELECT event_type, entity_id, issue_key, fired_at FROM jira_hook_dispatch WHERE delivery_id = ? ORDER BY fired_at, event_type, entity_id`,
		orchestrationID,
	)
	if err != nil {
		return nil, fmt.Errorf("delivery: list jira activity for %s: %w", orchestrationID, err)
	}
	defer rows.Close()

	out := []JiraActivity{}
	for rows.Next() {
		var eventType, entityID, issueKey, firedAt string
		if err := rows.Scan(&eventType, &entityID, &issueKey, &firedAt); err != nil {
			return nil, fmt.Errorf("delivery: scan jira activity for %s: %w", orchestrationID, err)
		}
		timestamp, err := time.Parse(time.RFC3339Nano, firedAt)
		if err != nil {
			return nil, fmt.Errorf("delivery: parse jira activity time for %s: %w", orchestrationID, err)
		}
		out = append(out, JiraActivity{EventType: eventType, EntityID: entityID, IssueKey: issueKey, FiredAt: timestamp})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("delivery: read jira activity for %s: %w", orchestrationID, err)
	}
	return out, nil
}

// newlyRunnableLaneIDs diffs current (every lane as of the full events
// replay) against the same lanes reduced from only the events with
// Sequence <= sinceSeq, and reports every lane id that is runnable now but
// was not (or did not exist) at that earlier point. allLanes is pure over
// whatever events slice it is given, so reducing it twice - once for the
// full log, once for a Sequence-filtered prefix - is enough to get "state
// as of sinceSeq" without a second DB read.
func newlyRunnableLaneIDs(orchestrationID string, events []protocol.DeliveryEvent, current map[string]*protocol.DeliveryLane, sinceSeq int) ([]string, error) {
	prior := make([]protocol.DeliveryEvent, 0, len(events))
	for _, ev := range events {
		if ev.Sequence <= sinceSeq {
			prior = append(prior, ev)
		}
	}
	priorLanes, err := allLanes(orchestrationID, prior)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(current))
	for id := range current {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := []string{}
	for _, id := range ids {
		l := current[id]
		if l.Status != protocol.DeliveryLaneStatusRunnable {
			continue
		}
		p, existed := priorLanes[id]
		if !existed || p.Status != protocol.DeliveryLaneStatusRunnable {
			out = append(out, id)
		}
	}
	return out, nil
}

// orchestrationTitle returns the label a consumer should show for a run
// instead of its opaque id: the title it was started with, or a derived
// one when it carries none. Deriving on read rather than stamping a
// title onto every orchestration at creation is what makes runs recorded
// before titles existed render a real label too - there is no backfill
// migration, because there is nothing persisted to backfill.
func orchestrationTitle(orch *protocol.DeliveryOrchestration, sources []*protocol.RequirementSource) string {
	if orch.Title != nil {
		if given := strings.TrimSpace(*orch.Title); given != "" {
			return given
		}
	}
	return deriveOrchestrationTitle(sources, orch.UnresolvedInputs)
}

// deriveOrchestrationTitle builds a readable label out of what a run
// already recorded about its own requirements, by a fixed rule rather
// than by interpreting anything: name the first requirement captured,
// and say how many others ride along with it.
//
// The first requirement is named by its captured title, else its
// summary, else its canonical key (which for a reference nobody enriched
// is just the reference string itself). A run whose only inputs are
// still-unanswered pending questions has no captured source to name, so
// it falls back to the first pending reference; a run with neither is
// counted rather than named. Nothing here inspects wording or tries to
// summarize - a derived title is always traceable back to exactly one
// requirement the caller passed in.
func deriveOrchestrationTitle(sources []*protocol.RequirementSource, pending []protocol.DeliveryOrchestrationUnresolvedInputsElem) string {
	total := len(sources) + len(pending)

	lead := ""
	switch {
	case len(sources) > 0:
		first := sources[0]
		candidates := []string{first.Title, "", first.CanonicalKey}
		if first.Summary != nil {
			candidates[1] = *first.Summary
		}
		for _, candidate := range candidates {
			if trimmed := strings.TrimSpace(candidate); trimmed != "" {
				lead = trimmed
				break
			}
		}
	case len(pending) > 0:
		lead = strings.TrimSpace(pending[0].Reference)
	}

	if lead == "" {
		if total == 0 {
			return "delivery with no requirements yet"
		}
		return fmt.Sprintf("delivery of %d requirements", total)
	}
	if total > 1 {
		return fmt.Sprintf("%s (+%d more)", lead, total-1)
	}
	return lead
}

// computeNextAction picks the single most useful next step, honestly
// and without trying to be clever: the first matching condition in a
// fixed priority order wins, and every other true condition is simply
// not mentioned this time - a caller polls get_delivery again once it
// acts on this one.
func computeNextAction(orch *protocol.DeliveryOrchestration, lanes []LaneSummary, pendingApprovals []*protocol.ApprovalManifest) string {
	if orch.Status == protocol.DeliveryOrchestrationStatusPending && len(orch.UnresolvedInputs) > 0 {
		refs := make([]string, 0, len(orch.UnresolvedInputs))
		for _, in := range orch.UnresolvedInputs {
			refs = append(refs, in.Reference)
		}
		return fmt.Sprintf("resolve %d pending question(s) via answer_delivery_question: %s", len(refs), strings.Join(refs, ", "))
	}
	if len(lanes) == 0 && len(pendingApprovals) == 0 {
		return "no lanes yet — decompose the delivery via register_project, create_parent_task, and create_lane"
	}
	if len(pendingApprovals) > 0 {
		return fmt.Sprintf("approve project delivery for project %s via approve_project_delivery", pendingApprovals[0].ProjectId)
	}

	var blocked, active, accepted, failed int
	for _, l := range lanes {
		if len(l.BlockedBy) > 0 {
			blocked++
		}
		switch l.Status {
		case protocol.DeliveryLaneStatusRunnable, protocol.DeliveryLaneStatusLeased, protocol.DeliveryLaneStatusRunning:
			active++
		case protocol.DeliveryLaneStatusAccepted:
			accepted++
		case protocol.DeliveryLaneStatusFailed:
			failed++
		}
	}
	if blocked > 0 {
		return fmt.Sprintf("waiting on %d blocked lane(s); no action needed, they unblock automatically when their dependency completes", blocked)
	}
	if active > 0 {
		return fmt.Sprintf("%d lane(s) actively in progress; delivery is progressing", active)
	}
	if len(lanes) > 0 && accepted == len(lanes) {
		return "delivery complete"
	}
	if failed > 0 {
		return fmt.Sprintf("%d lane(s) failed; needs human review", failed)
	}
	return "no pending action"
}

// StartDelivery bootstraps one new delivery orchestration from a batch
// of bare requirement reference strings. It is the one implementation
// the start_delivery MCP tool and the `punakawan deliver` CLI command
// both call, so the two surfaces can never drift apart: create the
// orchestration, classify each reference via ClassifyReference,
// capture every confidently classified one as a requirement source, and
// file every unclassifiable one as a pending question (an orchestration
// unresolved input) instead of failing the whole call over one
// ambiguous reference among otherwise-clear ones.
//
// idempotencyKey, if non-empty, makes a retried call with the same key
// resolve to the same orchestration rather than minting a second one:
// the orchestration id itself is derived deterministically from the
// key (contentDigest, already used elsewhere in this package for
// exact-match hashing), since CreateOrchestration's own duplicate-write
// short circuit only works if the retry looks up the same id the first
// call created. An empty key mints a fresh random id every call, since
// there is then nothing to derive the same id from on a hypothetical
// retry.
func (s *Store) StartDelivery(ctx context.Context, idempotencyKey string, references []string) (*DeliveryView, error) {
	return s.StartDeliveryWithOptions(ctx, idempotencyKey, references, OrchestrationOptions{})
}

// StartDeliveryWithOptions is StartDelivery plus the optional creation
// attributes in opts, threaded through to
// CreateOrchestrationWithOptions: a workflow definition whose Roles map
// customizes the new orchestration's role-stage gate, and a title. A
// zero opts behaves identically to StartDelivery - including for the
// title, which is left unset rather than derived and persisted here,
// since the requirement sources a derived title reads from are only
// captured further down this same function and can still change
// afterwards.
func (s *Store) StartDeliveryWithOptions(ctx context.Context, idempotencyKey string, references []string, opts OrchestrationOptions) (*DeliveryView, error) {
	orchestrationID := NewID()
	createKey := orchestrationID
	if idempotencyKey != "" {
		orchestrationID = contentDigest(idempotencyKey)[:26]
		createKey = idempotencyKey
	}

	var pending []protocol.DeliveryOrchestrationUnresolvedInputsElem
	var sources []SourceInput
	for _, ref := range references {
		if in, ok := ClassifyReference(ref); ok {
			sources = append(sources, in)
		} else {
			pending = append(pending, protocol.DeliveryOrchestrationUnresolvedInputsElem{Reference: ref})
		}
	}

	if _, err := s.CreateOrchestrationWithOptions(ctx, createKey, orchestrationID, pending, opts); err != nil {
		return nil, err
	}
	// Each capture uses its own fresh key rather than one derived from
	// idempotencyKey: CaptureRequirement already dedups a re-capture of
	// identical content by canonical_key+content hash (requirements.go),
	// so a retried StartDelivery call re-capturing the same sources is
	// already safe without also having to make each capture's own
	// idempotency key retry-stable.
	for _, in := range sources {
		if _, err := s.CaptureRequirement(ctx, NewID(), orchestrationID, in); err != nil {
			return nil, err
		}
	}
	return s.BuildDeliveryView(ctx, orchestrationID)
}
