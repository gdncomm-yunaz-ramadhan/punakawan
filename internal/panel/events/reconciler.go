package events

import (
	"context"
	"time"

	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// DefaultInterval is how often the fast tier (tier 1) polls, chosen to
// meet §18's "live update visible in UI under 1 second" for typical local
// corpora (§18: up to 20 workspaces, 10,000 sessions) without busy-polling.
const DefaultInterval = 1 * time.Second

// DefaultAvailabilityInterval is how often the slow tier (tier 2) polls
// workspace availability. Per the performance plan §10.2/§10.4, the deep
// Workspace.List inspection (Dolt/bd/git/adapter probes per workspace) is
// far too expensive to run every second: workspace availability changes
// rarely, so it moves to a 15s cadence. Session and approval detection
// stay on DefaultInterval.
const DefaultAvailabilityInterval = 15 * time.Second

// DefaultSubsystemInterval is how often the medium tier (tier 3) polls the
// three project-scoped subsystems (Contradiction Ledger, Impact Graph,
// Change Dossiers), per the role-config distinguished improvements plan §46.
// Their reads are per-project file loads over the
// .punakawan tree - cheaper than the deep Workspace.List probe but heavier
// than the in-memory session/approval checks, and their state changes on
// human/agent cadence (contradictions detected, dossiers finalized), not
// sub-second. A 2s cadence keeps the UI responsive without re-reading the
// ledger every tick.
const DefaultSubsystemInterval = 2 * time.Second

// Reconciler periodically polls panel.Readers and publishes a PanelEvent
// to Hub for whatever changed since the previous poll, per §12's source
// 4 ("periodic reconciliation").
//
// Polling is split into two tiers so the cheap, latency-sensitive checks
// (sessions, approvals) run every second while the expensive workspace
// availability inspection runs on a slower cadence:
//
//   - Tier 1 (Interval, default 1s): session + approval change detection.
//   - Tier 2 (AvailabilityInterval, default 15s): workspace availability
//     change detection, which triggers the deep per-workspace probes.
//   - Tier 3 (SubsystemInterval, default 2s): contradiction/impact/dossier
//     change detection over the per-project .punakawan tree (§46).
type Reconciler struct {
	Hub         *Hub
	Readers     panel.Readers
	WorkspaceID string
	Interval    time.Duration
	// AvailabilityInterval is tier 2's cadence. Zero selects
	// DefaultAvailabilityInterval.
	AvailabilityInterval time.Duration
	// SubsystemInterval is tier 3's cadence. Zero selects
	// DefaultSubsystemInterval.
	SubsystemInterval time.Duration

	prevSessions map[string]protocol.PanelSessionSummary
	// prevApprovals is keyed by project id, then approval id, so polling more
	// than one project (see reconcileFast) cannot collide two projects'
	// approval ids in one shared map.
	prevApprovals  map[string]map[string]protocol.ApprovalRecordStatus
	prevWorkspaces map[string]protocol.PanelSourceHealthAvailability

	// Tier-3 (subsystem) prev-state maps, one per project-scoped subsystem.
	prevContradictions map[string]protocol.ContradictionStatus
	prevDossiers       map[string]protocol.ChangeDossierStatus
	// prevImpactCount is the last observed impact node count. -1 means the
	// impact graph has not been polled yet (priming), so the first sighting
	// records the count without emitting a spurious snapshot_updated.
	prevImpactCount int
}

func strPtr(s string) *string { return &s }

// Run polls until ctx is cancelled. Call it in its own goroutine.
func (r *Reconciler) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	availInterval := r.AvailabilityInterval
	if availInterval <= 0 {
		availInterval = DefaultAvailabilityInterval
	}
	subsysInterval := r.SubsystemInterval
	if subsysInterval <= 0 {
		subsysInterval = DefaultSubsystemInterval
	}
	r.initState()

	r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSystemReady, OccurredAt: time.Now().UTC()})
	// Prime every tier once so first-sighting events fire immediately.
	r.reconcileFast(ctx)
	r.reconcileAvailability(ctx)
	r.reconcileSubsystems(ctx)

	fastTicker := time.NewTicker(interval)
	defer fastTicker.Stop()
	availTicker := time.NewTicker(availInterval)
	defer availTicker.Stop()
	subsysTicker := time.NewTicker(subsysInterval)
	defer subsysTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-fastTicker.C:
			r.reconcileFast(ctx)
		case <-availTicker.C:
			r.reconcileAvailability(ctx)
		case <-subsysTicker.C:
			r.reconcileSubsystems(ctx)
		}
	}
}

// initState (re)initialises every prev-state map. Run calls it; tests that
// drive reconcileOnce directly may call it too instead of hand-building the
// maps.
func (r *Reconciler) initState() {
	r.prevSessions = map[string]protocol.PanelSessionSummary{}
	r.prevApprovals = map[string]map[string]protocol.ApprovalRecordStatus{}
	r.prevWorkspaces = map[string]protocol.PanelSourceHealthAvailability{}
	r.prevContradictions = map[string]protocol.ContradictionStatus{}
	r.prevDossiers = map[string]protocol.ChangeDossierStatus{}
	r.prevImpactCount = -1
}

// reconcileOnce runs every tier in one pass. Retained for callers/tests
// that want a single synchronous reconciliation of everything.
func (r *Reconciler) reconcileOnce(ctx context.Context) {
	r.reconcileFast(ctx)
	r.reconcileAvailability(ctx)
	r.reconcileSubsystems(ctx)
}

// reconcileFast is tier 1: session + approval change detection. It never
// calls the expensive Workspace.List.
func (r *Reconciler) reconcileFast(ctx context.Context) {
	now := time.Now().UTC()

	if sessions, err := r.Readers.Session.List(ctx, r.WorkspaceID, contract.SessionFilter{}); err == nil {
		seen := make(map[string]bool, len(sessions))
		for _, s := range sessions {
			seen[s.Id] = true
			prev, existed := r.prevSessions[s.Id]
			switch {
			case !existed:
				r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSessionStarted, OccurredAt: now, WorkspaceId: strPtr(s.WorkspaceId), SessionId: strPtr(s.Id)})
			case prev.Status != s.Status:
				r.Hub.Publish(protocol.PanelEvent{Type: sessionStatusEventType(s.Status), OccurredAt: now, WorkspaceId: strPtr(s.WorkspaceId), SessionId: strPtr(s.Id)})
			case prev.UpdatedAt != s.UpdatedAt:
				r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeSessionProgress, OccurredAt: now, WorkspaceId: strPtr(s.WorkspaceId), SessionId: strPtr(s.Id)})
			}
			r.prevSessions[s.Id] = s
		}
		for id := range r.prevSessions {
			if !seen[id] {
				delete(r.prevSessions, id)
			}
		}
	}

	// Poll the primary plus whichever non-primary projects are already warm
	// in the runtime pool: a non-primary project's own CLI
	// approval resolution previously only reached the panel UI via an
	// unrelated ambient event, since this tier only ever polled the primary.
	// Piggybacking on already-loaded runtimes (rather than force-Acquiring
	// every registered project every tick) keeps the bounded-pool design
	// intact - a project nobody has opened in the panel recently still gets
	// no targeted push, but that also means nothing is rendering its
	// approvals list right now to need one.
	projects := []string{r.WorkspaceID}
	if r.Readers.Runtime != nil {
		projects = append(projects, r.Readers.Runtime.ActiveNonPrimaryIDs()...)
	}
	for _, projectID := range projects {
		r.reconcileApprovalsForProject(ctx, now, projectID)
	}
}

// reconcileApprovalsForProject polls one project's approvals and publishes
// approval.requested/approval.resolved for whatever changed since the last
// poll of that project, tracked in r.prevApprovals[projectID].
func (r *Reconciler) reconcileApprovalsForProject(ctx context.Context, now time.Time, projectID string) {
	pending, err := r.Readers.Approval.List(ctx, projectID, contract.ApprovalFilter{})
	if err != nil {
		return
	}
	prev, ok := r.prevApprovals[projectID]
	if !ok {
		prev = map[string]protocol.ApprovalRecordStatus{}
	}

	seen := make(map[string]bool, len(pending))
	for _, a := range pending {
		seen[a.Id] = true
		prevStatus, existed := prev[a.Id]
		if !existed && a.Status == protocol.ApprovalRecordStatusPending {
			r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeApprovalRequested, OccurredAt: now, WorkspaceId: strPtr(projectID), SessionId: strPtr(a.RunId), EntityId: strPtr(a.Id)})
		} else if existed && prevStatus != a.Status && a.Status != protocol.ApprovalRecordStatusPending {
			r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeApprovalResolved, OccurredAt: now, WorkspaceId: strPtr(projectID), SessionId: strPtr(a.RunId), EntityId: strPtr(a.Id)})
		}
		prev[a.Id] = a.Status
	}
	for id := range prev {
		if !seen[id] {
			delete(prev, id)
		}
	}
	r.prevApprovals[projectID] = prev
}

// reconcileAvailability is tier 2: workspace availability change
// detection. Workspace.List is the deep per-workspace probe (Dolt/bd/git/
// adapters) the performance plan §10.2 keeps off the 1s hot path, so this
// runs on the slower AvailabilityInterval cadence.
func (r *Reconciler) reconcileAvailability(ctx context.Context) {
	now := time.Now().UTC()

	if workspaces, err := r.Readers.Workspace.List(ctx); err == nil {
		for _, ws := range workspaces {
			if prev, existed := r.prevWorkspaces[ws.ID]; !existed || prev != ws.Availability {
				r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeWorkspaceAvailabilityChanged, OccurredAt: now, WorkspaceId: strPtr(ws.ID)})
			}
			r.prevWorkspaces[ws.ID] = ws.Availability
		}
	}
}

// reconcileSubsystems is tier 3: change detection for the three
// project-scoped subsystems (Contradiction Ledger, Impact Graph, Change
// Dossiers), per the role-config distinguished improvements plan §46.
// Project id == workspace id (a project shares its id with the workspace it
// is rooted in), so r.WorkspaceID is the project id passed to each reader.
//
// Every reader call is guarded with `if ..., err := ...; err == nil` so a
// project without one of these stores (or a workspace whose .punakawan tree
// lacks it) simply produces no events for that subsystem rather than
// crashing the tier.
//
// The plan's finer-grained impact.node_changed / impact.edge_changed are
// intentionally collapsed into a single impact.snapshot_updated here: the
// poll model only sees whole snapshots, not per-node/edge deltas, so the
// cheapest honest signal is "the node count changed since last poll" (the
// ImpactReader exposes ImpactNodes but no standalone edge list).
func (r *Reconciler) reconcileSubsystems(ctx context.Context) {
	now := time.Now().UTC()
	ws := r.WorkspaceID

	// Lazily initialise the tier-3 maps so callers that drive
	// reconcileOnce directly (tests) without going through Run's initState
	// still get non-nil maps.
	if r.prevContradictions == nil {
		r.prevContradictions = map[string]protocol.ContradictionStatus{}
	}
	if r.prevDossiers == nil {
		r.prevDossiers = map[string]protocol.ChangeDossierStatus{}
		r.prevImpactCount = -1
	}

	// Contradictions: detected on first sighting; resolved when the record
	// enters a terminal/settled status (resolved / accepted_divergence /
	// superseded); updated on any other status change.
	if r.Readers.Contradiction != nil {
		if list, err := r.Readers.Contradiction.ListContradictions(ctx, ws); err == nil {
			seen := make(map[string]bool, len(list))
			for _, c := range list {
				seen[c.Id] = true
				prev, existed := r.prevContradictions[c.Id]
				switch {
				case !existed:
					r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeContradictionDetected, OccurredAt: now, WorkspaceId: strPtr(ws), EntityId: strPtr(c.Id)})
				case prev != c.Status:
					if isResolvedContradiction(c.Status) {
						r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeContradictionResolved, OccurredAt: now, WorkspaceId: strPtr(ws), EntityId: strPtr(c.Id)})
					} else {
						r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeContradictionUpdated, OccurredAt: now, WorkspaceId: strPtr(ws), EntityId: strPtr(c.Id)})
					}
				}
				r.prevContradictions[c.Id] = c.Status
			}
			for id := range r.prevContradictions {
				if !seen[id] {
					delete(r.prevContradictions, id)
				}
			}
		}
	}

	// Dossiers: created on first sighting; finalized when it reaches
	// completed; status_changed on any other status change.
	if r.Readers.Dossier != nil {
		if list, err := r.Readers.Dossier.ListDossiers(ctx, ws); err == nil {
			seen := make(map[string]bool, len(list))
			for _, d := range list {
				seen[d.Id] = true
				prev, existed := r.prevDossiers[d.Id]
				switch {
				case !existed:
					r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeDossierCreated, OccurredAt: now, WorkspaceId: strPtr(ws), EntityId: strPtr(d.Id)})
				case prev != d.Status:
					if d.Status == protocol.ChangeDossierStatusCompleted {
						r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeDossierFinalized, OccurredAt: now, WorkspaceId: strPtr(ws), EntityId: strPtr(d.Id)})
					} else {
						r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeDossierStatusChanged, OccurredAt: now, WorkspaceId: strPtr(ws), EntityId: strPtr(d.Id)})
					}
				}
				r.prevDossiers[d.Id] = d.Status
			}
			for id := range r.prevDossiers {
				if !seen[id] {
					delete(r.prevDossiers, id)
				}
			}
		}
	}

	// Impact graph: cheap snapshot signal - emit snapshot_updated whenever
	// the node count changes. The first sighting only primes the count
	// (prevImpactCount == -1) so an initial graph does not fire a spurious
	// event.
	if r.Readers.Impact != nil {
		if nodes, err := r.Readers.Impact.ImpactNodes(ctx, ws); err == nil {
			count := len(nodes)
			if r.prevImpactCount >= 0 && count != r.prevImpactCount {
				r.Hub.Publish(protocol.PanelEvent{Type: protocol.PanelEventTypeImpactSnapshotUpdated, OccurredAt: now, WorkspaceId: strPtr(ws)})
			}
			r.prevImpactCount = count
		}
	}
}

// isResolvedContradiction reports whether status is a settled/terminal
// contradiction state that should surface as contradiction.resolved rather
// than contradiction.updated.
func isResolvedContradiction(status protocol.ContradictionStatus) bool {
	switch status {
	case protocol.ContradictionStatusResolved,
		protocol.ContradictionStatusAcceptedDivergence,
		protocol.ContradictionStatusSuperseded:
		return true
	default:
		return false
	}
}

func sessionStatusEventType(status string) protocol.PanelEventType {
	switch status {
	case "completed":
		return protocol.PanelEventTypeSessionCompleted
	case "failed":
		return protocol.PanelEventTypeSessionFailed
	default:
		return protocol.PanelEventTypeSessionPhaseChanged
	}
}
