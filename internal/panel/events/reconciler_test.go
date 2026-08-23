package events

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/internal/impact"
	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/runtime"
	"github.com/ygrip/punakawan/pkg/protocol"
)

type fakeWorkspaceReader struct {
	summaries []contract.WorkspaceSummary
}

func (f fakeWorkspaceReader) List(ctx context.Context) ([]contract.WorkspaceSummary, error) {
	return f.summaries, nil
}
func (f fakeWorkspaceReader) Get(ctx context.Context, id string) (contract.WorkspaceDetail, error) {
	return contract.WorkspaceDetail{}, errors.New("not implemented")
}

type fakeSessionReader struct {
	sessions []protocol.PanelSessionSummary
}

func (f *fakeSessionReader) List(ctx context.Context, workspaceID string, filter contract.SessionFilter) ([]protocol.PanelSessionSummary, error) {
	return f.sessions, nil
}
func (f *fakeSessionReader) Get(ctx context.Context, workspaceID, sessionID string) (contract.SessionDetail, error) {
	return contract.SessionDetail{}, errors.New("not implemented")
}

type fakeApprovalReader struct {
	records []protocol.ApprovalRecord
}

func (f *fakeApprovalReader) List(ctx context.Context, workspaceID string, filter contract.ApprovalFilter) ([]protocol.ApprovalRecord, error) {
	return f.records, nil
}

// countingWorkspaceReader records how many times List (the deep,
// expensive per-workspace probe) is invoked, so a test can prove it runs
// on the slow tier-2 cadence rather than every 1s tier-1 tick.
type countingWorkspaceReader struct {
	summaries []contract.WorkspaceSummary
	calls     atomic.Int64
}

func (f *countingWorkspaceReader) List(ctx context.Context) ([]contract.WorkspaceSummary, error) {
	f.calls.Add(1)
	return f.summaries, nil
}
func (f *countingWorkspaceReader) Get(ctx context.Context, id string) (contract.WorkspaceDetail, error) {
	return contract.WorkspaceDetail{}, errors.New("not implemented")
}

// countingSessionReader records how many times List is invoked.
type countingSessionReader struct {
	sessions []protocol.PanelSessionSummary
	calls    atomic.Int64
}

func (f *countingSessionReader) List(ctx context.Context, workspaceID string, filter contract.SessionFilter) ([]protocol.PanelSessionSummary, error) {
	f.calls.Add(1)
	return f.sessions, nil
}
func (f *countingSessionReader) Get(ctx context.Context, workspaceID, sessionID string) (contract.SessionDetail, error) {
	return contract.SessionDetail{}, errors.New("not implemented")
}

// fakeContradictionReader serves a mutable slice of contradictions; only
// ListContradictions is exercised by the reconciler, the rest satisfy the
// interface.
type fakeContradictionReader struct {
	records []protocol.Contradiction
}

func (f *fakeContradictionReader) ListContradictions(ctx context.Context, projectID string) ([]protocol.Contradiction, error) {
	return f.records, nil
}
func (f *fakeContradictionReader) GetContradiction(ctx context.Context, projectID, id string) (*protocol.Contradiction, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeContradictionReader) CreateContradiction(ctx context.Context, projectID string, c protocol.Contradiction) (*protocol.Contradiction, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeContradictionReader) ProposeContradictionResolution(ctx context.Context, projectID, id, proposedStatement, rationale string, requiresHumanConfirmation bool) (*protocol.Contradiction, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeContradictionReader) ResolveContradiction(ctx context.Context, projectID, id, statement, by string) (*protocol.Contradiction, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeContradictionReader) AcceptContradictionDivergence(ctx context.Context, projectID, id, by string) (*protocol.Contradiction, error) {
	return nil, errors.New("not implemented")
}

// fakeDossierReader serves a mutable slice of dossiers.
type fakeDossierReader struct {
	records []protocol.ChangeDossier
}

func (f *fakeDossierReader) ListDossiers(ctx context.Context, projectID string) ([]protocol.ChangeDossier, error) {
	return f.records, nil
}
func (f *fakeDossierReader) CreateDossier(ctx context.Context, projectID string, d protocol.ChangeDossier) (protocol.ChangeDossier, error) {
	return protocol.ChangeDossier{}, errors.New("not implemented")
}
func (f *fakeDossierReader) GetDossier(ctx context.Context, projectID, id string) (dossier.Loaded, error) {
	return dossier.Loaded{}, errors.New("not implemented")
}
func (f *fakeDossierReader) AddDossierClaim(ctx context.Context, projectID, id string, claim protocol.DossierClaim) (protocol.DossierClaim, error) {
	return protocol.DossierClaim{}, errors.New("not implemented")
}
func (f *fakeDossierReader) VerifyDossierClaim(ctx context.Context, projectID, id, claimID, byRole, note string) (protocol.DossierClaim, error) {
	return protocol.DossierClaim{}, errors.New("not implemented")
}
func (f *fakeDossierReader) DisputeDossierClaim(ctx context.Context, projectID, id, claimID, byRole, note string) (protocol.DossierClaim, error) {
	return protocol.DossierClaim{}, errors.New("not implemented")
}
func (f *fakeDossierReader) AddDossierEvidence(ctx context.Context, projectID, id string, ev protocol.DossierEvidence) (protocol.DossierEvidence, error) {
	return protocol.DossierEvidence{}, errors.New("not implemented")
}
func (f *fakeDossierReader) FinalizeDossier(ctx context.Context, projectID, id string) error {
	return errors.New("not implemented")
}
func (f *fakeDossierReader) ExportDossierMarkdown(ctx context.Context, projectID, id string) (string, error) {
	return "", errors.New("not implemented")
}
func (f *fakeDossierReader) ExportDossierJSON(ctx context.Context, projectID, id string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

// fakeImpactReader serves a mutable slice of impact nodes.
type fakeImpactReader struct {
	nodes []protocol.ImpactNode
}

func (f *fakeImpactReader) ImpactNodes(ctx context.Context, projectID string) ([]protocol.ImpactNode, error) {
	return f.nodes, nil
}
func (f *fakeImpactReader) ImpactNode(ctx context.Context, projectID, nodeID string) (protocol.ImpactNode, bool, error) {
	return protocol.ImpactNode{}, false, errors.New("not implemented")
}
func (f *fakeImpactReader) QueryImpact(ctx context.Context, projectID, subjectID string, depth int, include []string) (impact.ImpactResult, error) {
	return impact.ImpactResult{}, errors.New("not implemented")
}
func (f *fakeImpactReader) RefreshImpact(ctx context.Context, projectID string) error {
	return errors.New("not implemented")
}

func drain(t *testing.T, ch <-chan protocol.PanelEvent, n int) []protocol.PanelEvent {
	t.Helper()
	var out []protocol.PanelEvent
	for i := 0; i < n; i++ {
		select {
		case evt := <-ch:
			out = append(out, evt)
		case <-time.After(time.Second):
			t.Fatalf("timed out after %d/%d events", len(out), n)
		}
	}
	return out
}

func TestReconcilerEmitsSessionStartedForNewSession(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	sessions := &fakeSessionReader{}
	r := &Reconciler{
		Hub:         hub,
		Readers:     panel.Readers{Workspace: fakeWorkspaceReader{}, Session: sessions, Approval: &fakeApprovalReader{}},
		WorkspaceID: "ws-a",
	}
	r.prevSessions = map[string]protocol.PanelSessionSummary{}
	r.prevApprovals = map[string]map[string]protocol.ApprovalRecordStatus{}
	r.prevWorkspaces = map[string]protocol.PanelSourceHealthAvailability{}

	sessions.sessions = []protocol.PanelSessionSummary{{Id: "run-1", WorkspaceId: "ws-a", Status: "executing", UpdatedAt: time.Now().UTC()}}
	r.reconcileOnce(context.Background())

	events := drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeSessionStarted {
		t.Fatalf("Type = %q, want session.started", events[0].Type)
	}
	if events[0].SessionId == nil || *events[0].SessionId != "run-1" {
		t.Fatalf("SessionId = %v, want run-1", events[0].SessionId)
	}
}

func TestReconcilerEmitsSessionCompletedOnStatusChange(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	sessions := &fakeSessionReader{sessions: []protocol.PanelSessionSummary{{Id: "run-1", WorkspaceId: "ws-a", Status: "executing", UpdatedAt: time.Now().UTC()}}}
	r := &Reconciler{
		Hub:         hub,
		Readers:     panel.Readers{Workspace: fakeWorkspaceReader{}, Session: sessions, Approval: &fakeApprovalReader{}},
		WorkspaceID: "ws-a",
	}
	r.prevSessions = map[string]protocol.PanelSessionSummary{}
	r.prevApprovals = map[string]map[string]protocol.ApprovalRecordStatus{}
	r.prevWorkspaces = map[string]protocol.PanelSourceHealthAvailability{}

	r.reconcileOnce(context.Background()) // seeds prevSessions, emits session.started
	drain(t, ch, 1)

	sessions.sessions[0].Status = "completed"
	r.reconcileOnce(context.Background())

	events := drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeSessionCompleted {
		t.Fatalf("Type = %q, want session.completed", events[0].Type)
	}
}

func TestReconcilerEmitsApprovalRequestedThenResolved(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	approvals := &fakeApprovalReader{records: []protocol.ApprovalRecord{
		{Id: "appr-1", RunId: "run-1", Status: protocol.ApprovalRecordStatusPending},
	}}
	r := &Reconciler{
		Hub:         hub,
		Readers:     panel.Readers{Workspace: fakeWorkspaceReader{}, Session: &fakeSessionReader{}, Approval: approvals},
		WorkspaceID: "ws-a",
	}
	r.prevSessions = map[string]protocol.PanelSessionSummary{}
	r.prevApprovals = map[string]map[string]protocol.ApprovalRecordStatus{}
	r.prevWorkspaces = map[string]protocol.PanelSourceHealthAvailability{}

	r.reconcileOnce(context.Background())
	events := drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeApprovalRequested {
		t.Fatalf("Type = %q, want approval.requested", events[0].Type)
	}

	approvals.records[0].Status = protocol.ApprovalRecordStatusApproved
	r.reconcileOnce(context.Background())
	events = drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeApprovalResolved {
		t.Fatalf("Type = %q, want approval.resolved", events[0].Type)
	}
}

// perProjectApprovalReader returns a distinct record set per workspaceID, so
// a test can prove the reconciler polled more than one project rather than
// only its own r.WorkspaceID.
type perProjectApprovalReader struct {
	byProject map[string][]protocol.ApprovalRecord
}

func (f *perProjectApprovalReader) List(ctx context.Context, workspaceID string, filter contract.ApprovalFilter) ([]protocol.ApprovalRecord, error) {
	return f.byProject[workspaceID], nil
}

// TestReconcilerPollsActiveNonPrimaryProjectApprovals guards against a
// regression where a non-primary project's own CLI approval resolution
// never got a targeted approval.resolved push, because tier 1 only ever
// polled
// r.WorkspaceID (the primary). It must now also poll whichever non-primary
// projects are already warm in the runtime pool (Readers.Runtime), without
// forcing a cold project to load.
func TestReconcilerPollsActiveNonPrimaryProjectApprovals(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	mgr := runtime.NewManager("ws-a", &app.App{},
		runtime.WithLoader(func(path string) (*app.App, error) { return &app.App{}, nil }),
		runtime.WithCloser(func(*app.App) error { return nil }),
	)
	if _, _, err := mgr.Acquire(context.Background(), "ws-b", "/fake/path-b"); err != nil {
		t.Fatalf("Acquire ws-b: %v", err)
	}

	approvals := &perProjectApprovalReader{byProject: map[string][]protocol.ApprovalRecord{
		"ws-b": {{Id: "appr-b1", RunId: "run-b1", Status: protocol.ApprovalRecordStatusPending}},
	}}
	r := &Reconciler{
		Hub:         hub,
		Readers:     panel.Readers{Workspace: fakeWorkspaceReader{}, Session: &fakeSessionReader{}, Approval: approvals, Runtime: mgr},
		WorkspaceID: "ws-a",
	}
	r.prevSessions = map[string]protocol.PanelSessionSummary{}
	r.prevApprovals = map[string]map[string]protocol.ApprovalRecordStatus{}
	r.prevWorkspaces = map[string]protocol.PanelSourceHealthAvailability{}

	r.reconcileOnce(context.Background())
	events := drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeApprovalRequested {
		t.Fatalf("Type = %q, want approval.requested for ws-b's pending approval", events[0].Type)
	}
	if events[0].WorkspaceId == nil || *events[0].WorkspaceId != "ws-b" {
		t.Fatalf("WorkspaceId = %v, want ws-b", events[0].WorkspaceId)
	}

	approvals.byProject["ws-b"][0].Status = protocol.ApprovalRecordStatusApproved
	r.reconcileOnce(context.Background())
	events = drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeApprovalResolved {
		t.Fatalf("Type = %q, want approval.resolved pushed for the non-primary project", events[0].Type)
	}
	if events[0].WorkspaceId == nil || *events[0].WorkspaceId != "ws-b" {
		t.Fatalf("WorkspaceId = %v, want ws-b", events[0].WorkspaceId)
	}
}

func TestReconcilerEmitsWorkspaceAvailabilityChanged(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	ws := fakeWorkspaceReader{summaries: []contract.WorkspaceSummary{{ID: "ws-a", Availability: protocol.PanelSourceHealthAvailabilityAvailable}}}
	r := &Reconciler{
		Hub:         hub,
		Readers:     panel.Readers{Workspace: ws, Session: &fakeSessionReader{}, Approval: &fakeApprovalReader{}},
		WorkspaceID: "ws-a",
	}
	r.prevSessions = map[string]protocol.PanelSessionSummary{}
	r.prevApprovals = map[string]map[string]protocol.ApprovalRecordStatus{}
	r.prevWorkspaces = map[string]protocol.PanelSourceHealthAvailability{}

	r.reconcileOnce(context.Background())
	drain(t, ch, 1) // first sighting always emits

	ws.summaries[0].Availability = protocol.PanelSourceHealthAvailabilityUnavailable
	r.Readers.Workspace = ws
	r.reconcileOnce(context.Background())

	events := drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeWorkspaceAvailabilityChanged {
		t.Fatalf("Type = %q, want workspace.availability_changed", events[0].Type)
	}
}

// TestReconcilerWorkspaceListRunsOnSlowCadence proves the deep
// Workspace.List probe runs on the slow tier-2 cadence, not on every fast
// tier-1 tick: with a 10ms fast interval and a 1s availability interval,
// the fast Session.List fires many times while Workspace.List fires only
// once (the startup prime) over a ~150ms window.
func TestReconcilerWorkspaceListRunsOnSlowCadence(t *testing.T) {
	hub := NewHub()

	ws := &countingWorkspaceReader{summaries: []contract.WorkspaceSummary{{ID: "ws-a", Availability: protocol.PanelSourceHealthAvailabilityAvailable}}}
	sessions := &countingSessionReader{}
	r := &Reconciler{
		Hub:                  hub,
		Readers:              panel.Readers{Workspace: ws, Session: sessions, Approval: &fakeApprovalReader{}},
		WorkspaceID:          "ws-a",
		Interval:             10 * time.Millisecond,
		AvailabilityInterval: time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()

	sessionCalls := sessions.calls.Load()
	wsCalls := ws.calls.Load()

	// Fast tier ran many times over 150ms at a 10ms cadence.
	if sessionCalls < 5 {
		t.Fatalf("Session.List called %d times, want many (fast tier)", sessionCalls)
	}
	// Slow tier only ran on the startup prime; the 1s availability ticker
	// never fired within the 150ms window.
	if wsCalls != 1 {
		t.Fatalf("Workspace.List called %d times, want exactly 1 (slow tier, prime only)", wsCalls)
	}
	// And the deep probe ran far less often than the fast checks.
	if wsCalls >= sessionCalls {
		t.Fatalf("Workspace.List (%d) should run far less than Session.List (%d)", wsCalls, sessionCalls)
	}
}

// subsystemReconciler builds a Reconciler whose base (session/approval/
// workspace) readers are empty and whose three project-scoped subsystem
// readers are the supplied fakes, with all prev-state maps initialised.
func subsystemReconciler(hub *Hub, c *fakeContradictionReader, d *fakeDossierReader, im *fakeImpactReader) *Reconciler {
	r := &Reconciler{
		Hub: hub,
		Readers: panel.Readers{
			Workspace:     fakeWorkspaceReader{},
			Session:       &fakeSessionReader{},
			Approval:      &fakeApprovalReader{},
			Contradiction: c,
			Dossier:       d,
			Impact:        im,
		},
		WorkspaceID: "proj-a",
	}
	r.initState()
	return r
}

func TestReconcilerEmitsContradictionDetectedThenResolved(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	c := &fakeContradictionReader{records: []protocol.Contradiction{
		{Id: "con-1", Status: protocol.ContradictionStatusDetected},
	}}
	r := subsystemReconciler(hub, c, &fakeDossierReader{}, &fakeImpactReader{})

	r.reconcileOnce(context.Background())
	events := drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeContradictionDetected {
		t.Fatalf("Type = %q, want contradiction.detected", events[0].Type)
	}
	if events[0].EntityId == nil || *events[0].EntityId != "con-1" {
		t.Fatalf("EntityId = %v, want con-1", events[0].EntityId)
	}

	// A non-terminal status change surfaces contradiction.updated.
	c.records[0].Status = protocol.ContradictionStatusTriaged
	r.reconcileOnce(context.Background())
	events = drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeContradictionUpdated {
		t.Fatalf("Type = %q, want contradiction.updated", events[0].Type)
	}

	// Entering a terminal status surfaces contradiction.resolved.
	c.records[0].Status = protocol.ContradictionStatusResolved
	r.reconcileOnce(context.Background())
	events = drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeContradictionResolved {
		t.Fatalf("Type = %q, want contradiction.resolved", events[0].Type)
	}
}

func TestReconcilerEmitsDossierCreatedThenFinalized(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	d := &fakeDossierReader{records: []protocol.ChangeDossier{
		{Id: "dos-1", Status: protocol.ChangeDossierStatusDraft},
	}}
	r := subsystemReconciler(hub, &fakeContradictionReader{}, d, &fakeImpactReader{})

	r.reconcileOnce(context.Background())
	events := drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeDossierCreated {
		t.Fatalf("Type = %q, want dossier.created", events[0].Type)
	}

	// A non-terminal status change surfaces dossier.status_changed.
	d.records[0].Status = protocol.ChangeDossierStatusImplementing
	r.reconcileOnce(context.Background())
	events = drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeDossierStatusChanged {
		t.Fatalf("Type = %q, want dossier.status_changed", events[0].Type)
	}

	// Reaching completed surfaces dossier.finalized.
	d.records[0].Status = protocol.ChangeDossierStatusCompleted
	r.reconcileOnce(context.Background())
	events = drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeDossierFinalized {
		t.Fatalf("Type = %q, want dossier.finalized", events[0].Type)
	}
}

func TestReconcilerEmitsImpactSnapshotUpdatedOnCountChange(t *testing.T) {
	hub := NewHub()
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	im := &fakeImpactReader{nodes: []protocol.ImpactNode{{Id: "n1"}}}
	r := subsystemReconciler(hub, &fakeContradictionReader{}, &fakeDossierReader{}, im)

	// First poll only primes the count - no event.
	r.reconcileOnce(context.Background())
	select {
	case evt := <-ch:
		t.Fatalf("first poll emitted %q, want nothing (priming)", evt.Type)
	case <-time.After(50 * time.Millisecond):
	}

	// Node count changes -> snapshot_updated.
	im.nodes = append(im.nodes, protocol.ImpactNode{Id: "n2"})
	r.reconcileOnce(context.Background())
	events := drain(t, ch, 1)
	if events[0].Type != protocol.PanelEventTypeImpactSnapshotUpdated {
		t.Fatalf("Type = %q, want impact.snapshot_updated", events[0].Type)
	}
}
