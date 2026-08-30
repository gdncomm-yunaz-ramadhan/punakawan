package deliveryservice

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/telemetry"
)

func newServiceWithTelemetry(t *testing.T) (*Service, *telemetry.Store) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tstore := telemetry.NewStore(db)
	return New(delivery.NewStore(db), plan.NewStore(db), WithTelemetryStore(tstore)), tstore
}

func TestStartOrResolveBeginsTelemetrySessionWhenConfigured(t *testing.T) {
	svc, tstore := newServiceWithTelemetry(t)
	req := jiraRequest("tenant-a", "ABC-123", "session-a")
	req.Session.Provider = "claude-code"
	req.Session.ExternalSessionID = "claude-sess-1"
	result := mustStart(t, svc, req)

	if result.TelemetrySession == nil {
		t.Fatal("TelemetrySession = nil, want a begun telemetry session")
	}
	if result.TelemetrySession.ClientKind != "claude-code" || result.TelemetrySession.ExternalSessionID != "claude-sess-1" {
		t.Fatalf("telemetry session = %+v, want claude-code/claude-sess-1", result.TelemetrySession)
	}

	totals, err := tstore.TotalsByDelivery(context.Background(), result.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("TotalsByDelivery: %v", err)
	}
	if totals.OrchestrationID != result.Execution.OrchestrationID {
		t.Fatalf("totals orchestration id = %q, want %q", totals.OrchestrationID, result.Execution.OrchestrationID)
	}
}

func TestStartOrResolveWithoutTelemetryStoreLeavesTelemetrySessionNil(t *testing.T) {
	svc := newService(t) // no WithTelemetryStore
	result := mustStart(t, svc, jiraRequest("tenant-a", "ABC-999", "session-a"))
	if result.TelemetrySession != nil {
		t.Fatalf("TelemetrySession = %+v, want nil when no telemetry store is configured", result.TelemetrySession)
	}
}

func TestCompleteFinalizesStrayTelemetrySessions(t *testing.T) {
	svc, tstore := newServiceWithTelemetry(t)
	req := jiraRequest("tenant-a", "ABC-123", "session-a")
	req.Session.Provider = "claude-code"
	req.Session.ExternalSessionID = "claude-sess-1"
	result := mustStart(t, svc, req)

	mustComplete(t, svc, result.Execution.OrchestrationID)

	session, err := tstore.GetSessionByExternalID(context.Background(), "claude-code", "claude-sess-1")
	if err != nil {
		t.Fatalf("GetSessionByExternalID: %v", err)
	}
	if session.Status != "closed" {
		t.Fatalf("session status = %q, want closed after delivery completion", session.Status)
	}
	if session.StopReason != "delivery_completed" {
		t.Fatalf("stop reason = %q, want delivery_completed", session.StopReason)
	}
}

func TestCancelFinalizesStrayTelemetrySessions(t *testing.T) {
	svc, tstore := newServiceWithTelemetry(t)
	req := jiraRequest("tenant-a", "ABC-321", "session-a")
	req.Session.Provider = "codex"
	req.Session.ExternalSessionID = "thr-1"
	result := mustStart(t, svc, req)

	mustCancel(t, svc, result.Execution.OrchestrationID)

	session, err := tstore.GetSessionByExternalID(context.Background(), "codex", "thr-1")
	if err != nil {
		t.Fatalf("GetSessionByExternalID: %v", err)
	}
	if session.Status != "closed" || session.StopReason != "delivery_cancelled" {
		t.Fatalf("session = %+v, want closed/delivery_cancelled", session)
	}
}

func TestTwoSessionsOnSameJiraLifetimeAreAdditive(t *testing.T) {
	svc, tstore := newServiceWithTelemetry(t)
	firstReq := jiraRequest("tenant-a", "ABC-123", "session-a")
	firstReq.Session.Provider = "claude-code"
	firstReq.Session.ExternalSessionID = "claude-sess-1"
	first := mustStart(t, svc, firstReq)

	secondReq := jiraRequest("tenant-a", "ABC-123", "session-b")
	secondReq.Session.Provider = "codex"
	secondReq.Session.ExternalSessionID = "codex-thr-1"
	second := mustStart(t, svc, secondReq)

	if first.Execution.OrchestrationID != second.Execution.OrchestrationID {
		t.Fatalf("expected the same reused execution, got %q and %q", first.Execution.OrchestrationID, second.Execution.OrchestrationID)
	}
	if _, err := tstore.IngestSnapshot(context.Background(), telemetry.SnapshotRequest{SessionID: first.TelemetrySession.ID, SourceID: "main", Sequence: 1, InputTokens: 10}); err != nil {
		t.Fatalf("IngestSnapshot: %v", err)
	}
	if _, err := tstore.IngestSnapshot(context.Background(), telemetry.SnapshotRequest{SessionID: second.TelemetrySession.ID, SourceID: "main", Sequence: 1, InputTokens: 30}); err != nil {
		t.Fatalf("IngestSnapshot: %v", err)
	}

	totals, err := tstore.TotalsByDelivery(context.Background(), first.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("TotalsByDelivery: %v", err)
	}
	if totals.Counters.InputTokens != 40 {
		t.Fatalf("input tokens = %d, want 40 (additive across both sessions)", totals.Counters.InputTokens)
	}
}
