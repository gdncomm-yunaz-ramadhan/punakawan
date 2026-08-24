package planexec

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
)

func newTestStores(t *testing.T) (*Store, *plan.Store) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "planexec.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	plans := plan.NewStore(db)
	return NewStore(db, plans), plans
}

// TestReadinessEndToEnd is the required proof that this domain works in
// isolation: a plan with a dependent step is not ready while its
// dependency is open, becomes ready once the dependency is committed, and
// can then be claimed and completed - the exact lifecycle a follow-on
// migration off Beads would need to already work before it could rely on
// this package.
func TestReadinessEndToEnd(t *testing.T) {
	ctx := context.Background()
	store, plans := newTestStores(t)

	saved, err := plans.Save(ctx, plan.Plan{
		ID:        plan.NewID(),
		Objective: "two-step rollout",
		Steps: []plan.PlanStep{
			{ID: "step-a", Objective: "lay the groundwork"},
			{ID: "step-b", Objective: "build on the groundwork", DependsOn: []string{"step-a"}},
		},
	})
	if err != nil {
		t.Fatalf("plans.Save: %v", err)
	}

	execA, err := store.Create(ctx, saved.ID, saved.Revision, "step-a")
	if err != nil {
		t.Fatalf("Create step-a: %v", err)
	}
	execB, err := store.Create(ctx, saved.ID, saved.Revision, "step-b")
	if err != nil {
		t.Fatalf("Create step-b: %v", err)
	}

	// Before step-a is committed, step-b must not be ready: ListReady
	// omits it, Get reports it as blocked, and Claim refuses it.
	ready, err := store.ListReady(ctx, saved.ID)
	if err != nil {
		t.Fatalf("ListReady (before): %v", err)
	}
	if containsExecutionID(ready, execB.ID) {
		t.Fatalf("ListReady (before) = %+v, must not include step-b while step-a is open", ready)
	}
	if !containsExecutionID(ready, execA.ID) {
		t.Fatalf("ListReady (before) = %+v, must include step-a (no dependencies of its own)", ready)
	}

	gotB, err := store.Get(ctx, execB.ID)
	if err != nil {
		t.Fatalf("Get step-b (before): %v", err)
	}
	if gotB.Status != StatusBlocked {
		t.Fatalf("Get step-b (before).Status = %q, want %q", gotB.Status, StatusBlocked)
	}
	if _, err := store.Claim(ctx, execB.ID, "worker-1"); err == nil {
		t.Fatalf("Claim step-b (before): want error, step-a is not committed yet")
	}

	// Complete step-a: step-b must now be ready.
	completedA, err := store.Complete(ctx, execA.ID)
	if err != nil {
		t.Fatalf("Complete step-a: %v", err)
	}
	if completedA.Status != StatusCommitted {
		t.Fatalf("Complete step-a.Status = %q, want %q", completedA.Status, StatusCommitted)
	}

	readyAfter, err := store.ListReady(ctx, saved.ID)
	if err != nil {
		t.Fatalf("ListReady (after): %v", err)
	}
	if !containsExecutionID(readyAfter, execB.ID) {
		t.Fatalf("ListReady (after) = %+v, want step-b now that step-a is committed", readyAfter)
	}

	claimedB, err := store.Claim(ctx, execB.ID, "worker-1")
	if err != nil {
		t.Fatalf("Claim step-b (after): %v", err)
	}
	if claimedB.Status != StatusClaimed || claimedB.ClaimedBy != "worker-1" {
		t.Fatalf("Claim step-b (after) = %+v, want status claimed by worker-1", claimedB)
	}

	completedB, err := store.Complete(ctx, execB.ID)
	if err != nil {
		t.Fatalf("Complete step-b: %v", err)
	}
	if completedB.Status != StatusCommitted || completedB.CompletedAt == nil {
		t.Fatalf("Complete step-b = %+v, want committed with CompletedAt set", completedB)
	}
}

func TestCreateIsIdempotentPerPlanAndStep(t *testing.T) {
	ctx := context.Background()
	store, plans := newTestStores(t)

	saved, err := plans.Save(ctx, plan.Plan{
		ID: plan.NewID(), Objective: "one step",
		Steps: []plan.PlanStep{{ID: "only-step", Objective: "do the thing"}},
	})
	if err != nil {
		t.Fatalf("plans.Save: %v", err)
	}

	first, err := store.Create(ctx, saved.ID, saved.Revision, "only-step")
	if err != nil {
		t.Fatalf("Create (first): %v", err)
	}
	if _, err := store.Claim(ctx, first.ID, "worker-1"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	// Re-invoking the plan calls Create again for the same step - it must
	// return the same (now-claimed) execution, not a fresh Ready one.
	second, err := store.Create(ctx, saved.ID, saved.Revision, "only-step")
	if err != nil {
		t.Fatalf("Create (second): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("Create (second).ID = %q, want the same id as the first call %q", second.ID, first.ID)
	}
	if second.Status != StatusClaimed {
		t.Fatalf("Create (second).Status = %q, want %q (the state Claim already moved it to)", second.Status, StatusClaimed)
	}
}

func TestCreateRejectsUnknownStep(t *testing.T) {
	ctx := context.Background()
	store, plans := newTestStores(t)

	saved, err := plans.Save(ctx, plan.Plan{
		ID: plan.NewID(), Objective: "one step",
		Steps: []plan.PlanStep{{ID: "only-step", Objective: "do the thing"}},
	})
	if err != nil {
		t.Fatalf("plans.Save: %v", err)
	}
	if _, err := store.Create(ctx, saved.ID, saved.Revision, "no-such-step"); err == nil {
		t.Fatalf("Create with unknown step id: want error")
	}
}

func TestClaimRejectsAlreadyClaimed(t *testing.T) {
	ctx := context.Background()
	store, plans := newTestStores(t)

	saved, err := plans.Save(ctx, plan.Plan{
		ID: plan.NewID(), Objective: "one step",
		Steps: []plan.PlanStep{{ID: "only-step", Objective: "do the thing"}},
	})
	if err != nil {
		t.Fatalf("plans.Save: %v", err)
	}
	exec, err := store.Create(ctx, saved.ID, saved.Revision, "only-step")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Claim(ctx, exec.ID, "worker-1"); err != nil {
		t.Fatalf("Claim (first): %v", err)
	}
	if _, err := store.Claim(ctx, exec.ID, "worker-2"); err == nil {
		t.Fatalf("Claim (second, different worker): want error, already claimed")
	}
}

func TestReopenRequiresCommitted(t *testing.T) {
	ctx := context.Background()
	store, plans := newTestStores(t)

	saved, err := plans.Save(ctx, plan.Plan{
		ID: plan.NewID(), Objective: "one step",
		Steps: []plan.PlanStep{{ID: "only-step", Objective: "do the thing"}},
	})
	if err != nil {
		t.Fatalf("plans.Save: %v", err)
	}
	exec, err := store.Create(ctx, saved.ID, saved.Revision, "only-step")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Reopen(ctx, exec.ID, "found a regression"); err == nil {
		t.Fatalf("Reopen a not-yet-committed execution: want error")
	}

	if _, err := store.Complete(ctx, exec.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	reopened, err := store.Reopen(ctx, exec.ID, "found a regression")
	if err != nil {
		t.Fatalf("Reopen (after commit): %v", err)
	}
	if reopened.Status != StatusReopened || reopened.ReopenReason != "found a regression" {
		t.Fatalf("Reopen = %+v, want status reopened with the given reason", reopened)
	}
	if reopened.CompletedAt != nil {
		t.Fatalf("Reopen.CompletedAt = %v, want nil after reopening", reopened.CompletedAt)
	}

	// A reopened step is claimable again.
	if _, err := store.Claim(ctx, exec.ID, "worker-2"); err != nil {
		t.Fatalf("Claim after reopen: %v", err)
	}
}

func TestGetUnknownExecutionFails(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStores(t)
	if _, err := store.Get(ctx, "no-such-execution"); err == nil {
		t.Fatalf("Get unknown execution: want error")
	}
}

func containsExecutionID(execs []Execution, id string) bool {
	for _, e := range execs {
		if e.ID == id {
			return true
		}
	}
	return false
}
