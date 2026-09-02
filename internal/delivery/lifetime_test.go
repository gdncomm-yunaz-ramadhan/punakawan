package delivery

import (
	"context"
	"testing"
)

// TestJiraLifetimeIsOnePerIssueKeyRegardlessOfTenant pins the identity
// rule a delivery's whole audit trail rests on: a Jira issue is one piece
// of work. Resolving the same key through a differently-named adapter
// instance used to open a second, parallel delivery, which then had its
// own executions, sessions, worklogs and usage - none of it visible from
// the first.
func TestJiraLifetimeIsOnePerIssueKeyRegardlessOfTenant(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.StartOrResolveExecution(ctx, "first", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "gdn", Key: "TRF-1"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}

	second, err := s.StartOrResolveExecution(ctx, "second", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "gdncomm", Key: "TRF-1"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}

	if second.Lifetime.ID != first.Lifetime.ID {
		t.Fatalf("a differently-named tenant opened lifetime %s for TRF-1, want the existing %s", second.Lifetime.ID, first.Lifetime.ID)
	}
	if second.Execution.OrchestrationID != first.Execution.OrchestrationID {
		t.Fatalf("second call resolved to orchestration %s, want %s", second.Execution.OrchestrationID, first.Execution.OrchestrationID)
	}
	if second.CreatedLifetime || second.CreatedExecution {
		t.Fatalf("second call reported CreatedLifetime=%v CreatedExecution=%v, want it to have created nothing", second.CreatedLifetime, second.CreatedExecution)
	}
}

// TestCancelledJiraLifetimeReleasesItsKey covers the other half: a
// cancelled delivery must not block the issue forever, so the next start
// opens a fresh lifetime rather than resurrecting the cancelled one.
func TestCancelledJiraLifetimeReleasesItsKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.StartOrResolveExecution(ctx, "first", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "gdn", Key: "TRF-2"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	orch, err := s.GetOrchestration(ctx, first.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("GetOrchestration: %v", err)
	}
	if _, err := s.CancelOrchestration(ctx, "cancel", first.Execution.OrchestrationID, orch.Revision); err != nil {
		t.Fatalf("CancelOrchestration: %v", err)
	}

	next, err := s.StartOrResolveExecution(ctx, "next", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "gdn", Key: "TRF-2"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution after cancel: %v", err)
	}
	if next.Lifetime.ID == first.Lifetime.ID {
		t.Fatal("start after cancel reused the cancelled lifetime, want a fresh one")
	}
	if !next.CreatedLifetime {
		t.Fatal("start after cancel reported CreatedLifetime=false, want a new lifetime")
	}
}
