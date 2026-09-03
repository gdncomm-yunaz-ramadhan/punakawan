//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/deliveryservice"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/telemetry"
)

// startDaemonTransport binds a real daemon.Transport over s.deliveries and
// a real daemon.Client discovered against it exactly the way a live panel
// process would (port/token files, loopback HTTP), so this test proves the
// same served path the panel's own delivery routes proxy through -
// nothing here reads deliveries directly out of the store the way a
// shortcut in-process test would.
func startDaemonTransport(t *testing.T, s *stack) *daemon.Client {
	t.Helper()
	transport, err := daemon.NewTransport("127.0.0.1", "0", "test-token", nil, s.deliveries)
	if err != nil {
		t.Fatalf("daemon.NewTransport: %v", err)
	}
	go func() {
		_ = transport.Serve()
	}()
	t.Cleanup(func() {
		_ = transport.Shutdown(context.Background())
	})

	dir := t.TempDir()
	portPath := filepath.Join(dir, "daemon.port")
	tokenPath := filepath.Join(dir, "daemon.token")
	if err := os.WriteFile(portPath, []byte(transport.Addr()), 0o600); err != nil {
		t.Fatalf("write port file: %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	client, err := daemon.Discover(daemon.Paths{PortPath: portPath, TokenPath: tokenPath})
	if err != nil {
		t.Fatalf("daemon.Discover: %v", err)
	}
	return client
}

// listRevisionFor returns orchestrationID's ProjectionRevision as reported
// by a fresh ListDeliveries call, failing the test if it is not present.
func listRevisionFor(t *testing.T, client *daemon.Client, orchestrationID string) int {
	t.Helper()
	list, err := client.ListDeliveries(context.Background())
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	for _, item := range list.Items {
		if item.ID == orchestrationID {
			return item.ProjectionRevision
		}
	}
	t.Fatalf("ListDeliveries did not include %s", orchestrationID)
	return 0
}

// TestPanelProjectionRefresh proves the panel-facing projection - served
// over the same real daemon.Transport/daemon.Client the panel's own
// delivery routes proxy through, not a direct in-process store read -
// reflects externally mutated progress and provider-write state at a
// stable revision, and converges list and detail on a newer revision
// once a usage snapshot (the mutation that actually advances
// delivery_projection_versions) lands.
func TestPanelProjectionRefresh(t *testing.T) {
	ctx := context.Background()
	s := newStack(t)
	svc := s.deliveryService()

	start, needInput, err := svc.StartOrResolve(ctx, deliveryservice.StartRequest{
		IdempotencyKey: "start-panel-1",
		Source:         &deliveryservice.SourceIdentity{Kind: deliveryservice.SourceAdhoc},
		Title:          "Panel projection smoke",
		HighLevelPlan:  deliveryservice.PlanDraft{Objective: "Panel projection smoke"},
		Session:        deliveryservice.SessionStart{Participant: "agent-1"},
	})
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needInput != nil {
		t.Fatalf("StartOrResolve needed input: %+v", needInput)
	}
	if start.Session == nil || start.TelemetrySession == nil {
		t.Fatalf("expected both a delivery session and a telemetry session to open")
	}
	orchestrationID := start.Execution.OrchestrationID

	client := startDaemonTransport(t, s)

	baseline, err := client.GetDeliveryDetail(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("GetDeliveryDetail (baseline): %v", err)
	}
	baseRevision := baseline.ProjectionRevision
	if got := listRevisionFor(t, client, orchestrationID); got != baseRevision {
		t.Fatalf("list revision %d != detail revision %d at baseline", got, baseRevision)
	}

	// Mutate progress externally: visible immediately in detail, at the
	// same revision (ReportProgress never advances
	// delivery_projection_versions itself).
	half := 50.0
	if _, err := s.deliveries.ReportProgress(ctx, "progress-1", start.Session.ID, "", "halfway through the investigation", &half); err != nil {
		t.Fatalf("ReportProgress: %v", err)
	}
	afterProgress, err := client.GetDeliveryDetail(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("GetDeliveryDetail (after progress): %v", err)
	}
	if afterProgress.Progress == nil || afterProgress.Progress.Summary != "halfway through the investigation" {
		t.Fatalf("expected the reported progress to be visible in detail, got %+v", afterProgress.Progress)
	}
	if afterProgress.ProjectionRevision != baseRevision {
		t.Fatalf("progress revision = %d, want unchanged from baseline %d", afterProgress.ProjectionRevision, baseRevision)
	}

	// Mutate outbox state externally: a freshly enqueued provider write
	// intent is visible immediately in detail, at the same revision
	// (enqueueing never advances delivery_projection_versions itself).
	if _, err := s.outbox.Enqueue(ctx, outbox.Intent{
		OrchestrationID: orchestrationID, AdapterID: "atlassian", Operation: "atlassian.addJiraComment",
		TargetKey: "PANEL-1", PayloadJSON: `{"comment_body":"panel projection smoke"}`,
		OperationFingerprint: "panel-projection-smoke-comment",
	}); err != nil {
		t.Fatalf("outbox.Enqueue: %v", err)
	}
	afterOutbox, err := client.GetDeliveryDetail(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("GetDeliveryDetail (after outbox): %v", err)
	}
	if len(afterOutbox.ProviderWrites) != 1 || afterOutbox.ProviderWrites[0].Status != string(outbox.StatusPending) {
		t.Fatalf("expected exactly one pending provider write, got %+v", afterOutbox.ProviderWrites)
	}
	if afterOutbox.ProjectionRevision != baseRevision {
		t.Fatalf("outbox revision = %d, want unchanged from baseline %d", afterOutbox.ProjectionRevision, baseRevision)
	}

	// Mutate usage: this is the write that actually advances
	// delivery_projection_versions - list and detail must converge on the
	// same newer revision from a plain re-fetch, with no navigation.
	if _, err := s.telemetry.IngestSnapshot(ctx, telemetry.SnapshotRequest{
		SessionID: start.TelemetrySession.ID, SourceID: "main", Sequence: 1,
		InputTokens: 1200, OutputTokens: 300,
	}); err != nil {
		t.Fatalf("IngestSnapshot: %v", err)
	}
	afterUsage, err := client.GetDeliveryDetail(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("GetDeliveryDetail (after usage): %v", err)
	}
	if afterUsage.ProjectionRevision <= baseRevision {
		t.Fatalf("usage revision = %d, want greater than baseline %d", afterUsage.ProjectionRevision, baseRevision)
	}
	if afterUsage.Usage.InputTokens != 1200 || afterUsage.Usage.OutputTokens != 300 {
		t.Fatalf("expected the ingested usage to be visible in detail, got %+v", afterUsage.Usage)
	}
	if got := listRevisionFor(t, client, orchestrationID); got != afterUsage.ProjectionRevision {
		t.Fatalf("list revision %d != detail revision %d after usage mutation", got, afterUsage.ProjectionRevision)
	}
	// Progress and the provider write mutated earlier are still visible at
	// the new, higher revision - a revision bump never drops content an
	// earlier read already observed.
	if afterUsage.Progress == nil || afterUsage.Progress.Summary != "halfway through the investigation" {
		t.Fatalf("expected progress to remain visible after the revision advanced, got %+v", afterUsage.Progress)
	}
	if len(afterUsage.ProviderWrites) != 1 {
		t.Fatalf("expected the provider write to remain visible after the revision advanced, got %+v", afterUsage.ProviderWrites)
	}

	// Completing the delivery advances the revision again, through the
	// same served path.
	orch, err := s.deliveries.GetOrchestration(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("GetOrchestration: %v", err)
	}
	if _, err := svc.Complete(ctx, "complete-panel-1", orchestrationID, orch.Revision); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	afterComplete, err := client.GetDeliveryDetail(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("GetDeliveryDetail (after complete): %v", err)
	}
	if afterComplete.ProjectionRevision <= afterUsage.ProjectionRevision {
		t.Fatalf("completion revision = %d, want greater than %d", afterComplete.ProjectionRevision, afterUsage.ProjectionRevision)
	}
	if got := listRevisionFor(t, client, orchestrationID); got != afterComplete.ProjectionRevision {
		t.Fatalf("list revision %d != detail revision %d after completion", got, afterComplete.ProjectionRevision)
	}
}
