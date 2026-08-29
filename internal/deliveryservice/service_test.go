package deliveryservice

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(delivery.NewStore(db), plan.NewStore(db))
}

func jiraRequest(tenant, key, session string) StartRequest {
	return StartRequest{
		IdempotencyKey: "start-" + session,
		Source:         &SourceIdentity{Kind: SourceJira, Provider: "jira", Tenant: tenant, Key: key},
		Title:          "Deliver " + key,
		Session:        SessionStart{Participant: session},
	}
}

func adhocRequest(prompt, session string) StartRequest {
	return StartRequest{
		IdempotencyKey: "start-" + session,
		Source:         &SourceIdentity{Kind: SourceAdhoc},
		Title:          prompt,
		Session:        SessionStart{Participant: session},
	}
}

func mustStart(t *testing.T, svc *Service, req StartRequest) StartResult {
	t.Helper()
	result, needsInput, err := svc.StartOrResolve(context.Background(), req)
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needsInput != nil {
		t.Fatalf("StartOrResolve returned needs_input: %+v", needsInput)
	}
	return result
}

func mustComplete(t *testing.T, svc *Service, orchestrationID string) {
	t.Helper()
	orch, err := svc.deliveries.GetOrchestration(context.Background(), orchestrationID)
	if err != nil {
		t.Fatalf("GetOrchestration: %v", err)
	}
	if _, err := svc.Complete(context.Background(), "complete-"+orchestrationID, orchestrationID, orch.Revision); err != nil {
		t.Fatalf("Complete: %v", err)
	}
}

func mustCancel(t *testing.T, svc *Service, orchestrationID string) {
	t.Helper()
	orch, err := svc.deliveries.GetOrchestration(context.Background(), orchestrationID)
	if err != nil {
		t.Fatalf("GetOrchestration: %v", err)
	}
	if _, err := svc.Cancel(context.Background(), "cancel-"+orchestrationID, orchestrationID, orch.Revision); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
}

func TestJiraStartReusesLifetimeButCreatesNextExecutionAfterCompletion(t *testing.T) {
	svc := newService(t)
	first := mustStart(t, svc, jiraRequest("tenant-a", "abc-123", "session-a"))
	second := mustStart(t, svc, jiraRequest("tenant-a", "ABC-123", "session-b"))
	if first.Lifetime.ID != second.Lifetime.ID {
		t.Fatalf("Lifetime.ID = %q, want %q (reused)", second.Lifetime.ID, first.Lifetime.ID)
	}
	if first.Execution.ID != second.Execution.ID {
		t.Fatalf("Execution.ID = %q, want %q (reused active execution)", second.Execution.ID, first.Execution.ID)
	}

	mustComplete(t, svc, first.Execution.OrchestrationID)
	continued := mustStart(t, svc, jiraRequest("tenant-a", "ABC-123", "session-c"))
	if continued.Lifetime.ID != first.Lifetime.ID {
		t.Fatalf("continued Lifetime.ID = %q, want %q (same lifetime)", continued.Lifetime.ID, first.Lifetime.ID)
	}
	if continued.Execution.Ordinal != 2 {
		t.Fatalf("continued Execution.Ordinal = %d, want 2", continued.Execution.Ordinal)
	}
}

func TestCancelledJiraLifetimeIsNotReused(t *testing.T) {
	svc := newService(t)
	first := mustStart(t, svc, jiraRequest("tenant-a", "ABC-123", "session-a"))
	mustCancel(t, svc, first.Execution.OrchestrationID)
	second := mustStart(t, svc, jiraRequest("tenant-a", "ABC-123", "session-b"))
	if first.Lifetime.ID == second.Lifetime.ID {
		t.Fatalf("Lifetime.ID = %q, want a different lifetime after cancellation", second.Lifetime.ID)
	}
}

func TestAdhocStartAlwaysCreatesNewLifetime(t *testing.T) {
	svc := newService(t)
	a := mustStart(t, svc, adhocRequest("same prompt", "session-a"))
	b := mustStart(t, svc, adhocRequest("same prompt", "session-b"))
	if a.Lifetime.ID == b.Lifetime.ID {
		t.Fatalf("Lifetime.ID = %q, want a different lifetime for each ad-hoc start", b.Lifetime.ID)
	}
}
