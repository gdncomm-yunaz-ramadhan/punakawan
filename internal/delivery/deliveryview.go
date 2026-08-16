// deliveryview.go assembles DeliveryView, the one read model the six
// start-delivery facade tools share instead of each hand-rolling its own
// aggregation over lanes, tasks, edges, and manifests. It never mutates
// anything - every field is derived by
// replaying the orchestration's own event log, exactly like every
// other Get*/List* method in this package.
package delivery

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ProjectSummary rolls up one project's lanes within an orchestration:
// which lanes belong to it and how many sit in each scheduling status.
type ProjectSummary struct {
	ProjectID      string                              `json:"project_id"`
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
}

// BlockerSummary names one lane still waiting on unresolved
// predecessor task ids.
type BlockerSummary struct {
	LaneID       string   `json:"lane_id"`
	ParentTaskID string   `json:"parent_task_id,omitempty"`
	BlockedBy    []string `json:"blocked_by"`
}

// DeliveryView is the bounded, human-and-agent-readable snapshot of one
// orchestration: its own state, every lane grouped by project, every
// still-blocked lane, every pending approval, every pending question,
// and one honest sentence about what to do next.
type DeliveryView struct {
	Orchestration    *protocol.DeliveryOrchestration `json:"orchestration"`
	Projects         []ProjectSummary                `json:"projects"`
	Lanes            []LaneSummary                   `json:"lanes"`
	Blockers         []BlockerSummary                `json:"blockers"`
	PendingApprovals []*protocol.ApprovalManifest    `json:"pending_approvals"`
	PendingQuestions []string                        `json:"pending_questions"`
	NextAction       string                          `json:"next_action"`

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

	view := &DeliveryView{
		Orchestration:        orch,
		Projects:             []ProjectSummary{},
		Lanes:                []LaneSummary{},
		Blockers:             []BlockerSummary{},
		PendingApprovals:     []*protocol.ApprovalManifest{},
		PendingQuestions:     []string{},
		NewlyRunnableLaneIDs: []string{},
	}
	for _, ev := range events {
		if ev.Sequence > view.LatestSeq {
			view.LatestSeq = ev.Sequence
		}
	}

	laneIDs := make([]string, 0, len(laneMap))
	for id := range laneMap {
		laneIDs = append(laneIDs, id)
	}
	sort.Strings(laneIDs)

	laneIDsByProject := map[string][]string{}
	countsByProject := map[string]map[protocol.DeliveryLaneStatus]int{}

	for _, id := range laneIDs {
		l := laneMap[id]
		summary := LaneSummary{
			LaneID:    l.Id,
			ProjectID: l.ProjectId,
			Status:    l.Status,
			BlockedBy: l.BlockedBy,
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

	projectIDs := make([]string, 0, len(laneIDsByProject))
	for id := range laneIDsByProject {
		projectIDs = append(projectIDs, id)
	}
	sort.Strings(projectIDs)
	for _, id := range projectIDs {
		view.Projects = append(view.Projects, ProjectSummary{
			ProjectID:      id,
			LaneIDs:        laneIDsByProject[id],
			CountsByStatus: countsByProject[id],
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

// computeNextAction picks the single most useful next step, honestly
// and without trying to be clever: the first matching condition in a
// fixed priority order wins, and every other true condition is simply
// not mentioned this time - a caller polls again (get_delivery/
// resume_delivery are the same call) once it acts on this one.
func computeNextAction(orch *protocol.DeliveryOrchestration, lanes []LaneSummary, pendingApprovals []*protocol.ApprovalManifest) string {
	if orch.Status == protocol.DeliveryOrchestrationStatusPending && len(orch.UnresolvedInputs) > 0 {
		refs := make([]string, 0, len(orch.UnresolvedInputs))
		for _, in := range orch.UnresolvedInputs {
			refs = append(refs, in.Reference)
		}
		return fmt.Sprintf("resolve %d pending question(s) via answer_delivery_question: %s", len(refs), strings.Join(refs, ", "))
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
	return s.StartDeliveryWithDefinition(ctx, idempotencyKey, references, "")
}

// StartDeliveryWithDefinition is StartDelivery plus an optional
// workflowDefinitionID, threaded through to
// CreateOrchestrationWithDefinition so the new orchestration's role-stage
// gate can be customized by that definition's Roles map. An empty
// workflowDefinitionID behaves identically to StartDelivery.
func (s *Store) StartDeliveryWithDefinition(ctx context.Context, idempotencyKey string, references []string, workflowDefinitionID string) (*DeliveryView, error) {
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

	if _, err := s.CreateOrchestrationWithDefinition(ctx, createKey, orchestrationID, pending, workflowDefinitionID); err != nil {
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
