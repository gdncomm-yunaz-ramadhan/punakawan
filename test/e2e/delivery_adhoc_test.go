//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/deliveryservice"
	"github.com/ygrip/punakawan/internal/telemetry"
)

// TestDeliveryAdhocIsolation starts two ad-hoc deliveries with identical
// title/description text and no resume token, proving each opens its own
// independent lifetime, execution, plan, and session/usage projection -
// unlike a Jira source, identical ad-hoc text must never be treated as
// the same delivery - and that neither one ever captures a Jira artifact.
func TestDeliveryAdhocIsolation(t *testing.T) {
	ctx := context.Background()
	s := newStack(t)
	svc := s.deliveryService()

	start := func(idempotencyKey string) deliveryservice.StartResult {
		result, needInput, err := svc.StartOrResolve(ctx, deliveryservice.StartRequest{
			IdempotencyKey: idempotencyKey,
			Source:         &deliveryservice.SourceIdentity{Kind: deliveryservice.SourceAdhoc},
			Title:          "Investigate flaky checkout test",
			Description:    "The checkout suite fails intermittently on CI.",
			Session:        deliveryservice.SessionStart{Participant: "agent-1"},
			HighLevelPlan:  deliveryservice.PlanDraft{Title: "Investigate flaky checkout test", Content: "Reproduce, bisect, fix."},
		})
		if err != nil {
			t.Fatalf("StartOrResolve(%s): %v", idempotencyKey, err)
		}
		if needInput != nil {
			t.Fatalf("StartOrResolve(%s) needed input: %+v", idempotencyKey, needInput)
		}
		return result
	}

	first := start("adhoc-1")
	second := start("adhoc-2")

	if first.Lifetime.ID == second.Lifetime.ID {
		t.Fatalf("expected two ad-hoc starts with identical text to open distinct lifetimes, both got %s", first.Lifetime.ID)
	}
	if first.Execution.OrchestrationID == second.Execution.OrchestrationID {
		t.Fatalf("expected distinct orchestrations, both got %s", first.Execution.OrchestrationID)
	}
	if first.Session == nil || second.Session == nil {
		t.Fatalf("expected both starts to open a delivery session")
	}
	if first.Session.ID == second.Session.ID {
		t.Fatalf("expected distinct delivery sessions, both got %s", first.Session.ID)
	}
	if first.TelemetrySession == nil || second.TelemetrySession == nil {
		t.Fatalf("expected both starts to open a telemetry session")
	}

	// plan isolation: each orchestration has its own linked high-level
	// plan revision, not a shared one.
	if len(first.Reconciliation.Plans) == 0 || len(second.Reconciliation.Plans) == 0 {
		t.Fatalf("expected both starts to save a high-level plan, got %+v / %+v", first.Reconciliation.Plans, second.Reconciliation.Plans)
	}
	if first.Reconciliation.Plans[0] == second.Reconciliation.Plans[0] {
		t.Fatalf("expected distinct plan revisions, both got %s", first.Reconciliation.Plans[0])
	}

	// usage isolation: usage recorded against the first delivery's
	// telemetry session must never be visible in the second's totals.
	if _, err := s.telemetry.IngestSnapshot(ctx, telemetry.SnapshotRequest{
		SessionID: first.TelemetrySession.ID, SourceID: "main", Sequence: 1,
		InputTokens: 1000, OutputTokens: 500,
	}); err != nil {
		t.Fatalf("IngestSnapshot: %v", err)
	}
	firstTotals, err := s.telemetry.TotalsByDelivery(ctx, first.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("TotalsByDelivery(first): %v", err)
	}
	if firstTotals.Counters.InputTokens != 1000 || firstTotals.Counters.OutputTokens != 500 {
		t.Fatalf("first delivery totals = %+v, want 1000 input / 500 output", firstTotals.Counters)
	}
	secondTotals, err := s.telemetry.TotalsByDelivery(ctx, second.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("TotalsByDelivery(second): %v", err)
	}
	if secondTotals.Counters.InputTokens != 0 || secondTotals.Counters.OutputTokens != 0 {
		t.Fatalf("expected the second, unrelated delivery's usage totals to stay zero, got %+v", secondTotals.Counters)
	}

	// no Jira artifacts: an ad-hoc delivery never has a Jira lifecycle or
	// any captured requirement source.
	for name, orchestrationID := range map[string]string{"first": first.Execution.OrchestrationID, "second": second.Execution.OrchestrationID} {
		sources, err := s.deliveries.ListRequirementSources(ctx, orchestrationID)
		if err != nil {
			t.Fatalf("ListRequirementSources(%s): %v", name, err)
		}
		if len(sources) != 0 {
			t.Fatalf("expected the %s ad-hoc delivery to capture no requirement sources, got %d", name, len(sources))
		}
		lifecycle, err := s.deliveries.GetDeliveryLifecycle(ctx, orchestrationID)
		if err != nil {
			t.Fatalf("GetDeliveryLifecycle(%s): %v", name, err)
		}
		if lifecycle.Case.SourceKind != "adhoc" || lifecycle.Case.JiraIssueKey != "" {
			t.Fatalf("expected the %s delivery's case to be a Jira-less ad-hoc case, got %+v", name, lifecycle.Case)
		}
		if len(lifecycle.Snapshots) != 0 || len(lifecycle.WorkItems) != 0 || len(lifecycle.WriteIntents) != 0 {
			t.Fatalf("expected the %s ad-hoc delivery to carry no Jira snapshots, work items, or write intents", name)
		}
	}
}
