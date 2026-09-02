package delivery

import (
	"context"
	"testing"
)

// TestJiraLifetimeIsOnePerIssueKeyPerOrganisation pins the identity rule
// a delivery's whole audit trail rests on: one Jira issue is one piece of
// work. Two sites can issue the same key, so the organisation is part of
// that identity - but an organisation is derived from the site URL rather
// than typed, so it cannot be two spellings of the same place.
func TestJiraLifetimeIsOnePerIssueKeyPerOrganisation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.StartOrResolveExecution(ctx, "first", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "gdncomm", Key: "TRF-1"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}

	same, err := s.StartOrResolveExecution(ctx, "same", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "gdncomm", Key: "TRF-1"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	if same.Lifetime.ID != first.Lifetime.ID || same.CreatedLifetime || same.CreatedExecution {
		t.Fatalf("re-starting the same issue at the same organisation created %+v, want the existing lifetime %s", same, first.Lifetime.ID)
	}

	// PAY-1 at one company is not PAY-1 at another, so the same key at a
	// different organisation is different work.
	other, err := s.StartOrResolveExecution(ctx, "other", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "acme", Key: "TRF-1"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	if other.Lifetime.ID == first.Lifetime.ID {
		t.Fatal("a second organisation's identically-keyed issue collapsed into the first organisation's delivery")
	}
}

// TestJiraLifetimeStartedWithoutAnOrganisationIsAdopted covers every
// delivery that already exists: it was started before this host resolved
// an organisation at all, and the first organisation to name it must
// claim it rather than open a second delivery beside it.
func TestJiraLifetimeStartedWithoutAnOrganisationIsAdopted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.StartOrResolveExecution(ctx, "first", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Key: "TRF-3"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}

	adopted, err := s.StartOrResolveExecution(ctx, "adopt", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "gdncomm", Key: "TRF-3"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	if adopted.Lifetime.ID != first.Lifetime.ID {
		t.Fatalf("an organisation-less lifetime was duplicated as %s instead of adopted from %s", adopted.Lifetime.ID, first.Lifetime.ID)
	}
	if adopted.Lifetime.SourceTenant != "gdncomm" {
		t.Errorf("adopted lifetime tenant = %q, want gdncomm", adopted.Lifetime.SourceTenant)
	}
	// Adopted once, it is that organisation's - a different one is now
	// different work.
	other, err := s.StartOrResolveExecution(ctx, "other", SourceIdentity{Kind: SourceKindJira, Provider: "jira", Tenant: "acme", Key: "TRF-3"}, OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	if other.Lifetime.ID == first.Lifetime.ID {
		t.Fatal("a second organisation reused the already-adopted lifetime")
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
