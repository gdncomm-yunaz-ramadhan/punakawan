package events

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/contract"
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
	r.prevApprovals = map[string]protocol.ApprovalRecordStatus{}
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
	r.prevApprovals = map[string]protocol.ApprovalRecordStatus{}
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
	r.prevApprovals = map[string]protocol.ApprovalRecordStatus{}
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
	r.prevApprovals = map[string]protocol.ApprovalRecordStatus{}
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
