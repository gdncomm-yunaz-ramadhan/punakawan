package events

import (
	"context"
	"errors"
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
