package deliveryservice

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jirahooks"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
)

// fakeJiraIssue is one Jira issue a fakeJiraHydrator knows how to
// hydrate, plus whichever subtasks it currently has - a test mutates
// Subtasks between two StartOrResolve calls to simulate a newly
// discovered child.
type fakeJiraIssue struct {
	key      string
	subtasks []string
}

// WithSubtasks replaces this issue's known subtasks, matching the plan
// text's jira.Issue("ABC-123").WithSubtasks("ABC-124", "ABC-125") shape.
func (i *fakeJiraIssue) WithSubtasks(keys ...string) *fakeJiraIssue {
	i.subtasks = keys
	return i
}

// fakeJiraHydrator is a JiraHydrator that never talks to a real Jira
// adapter: it looks up the execution's own case (exactly like
// jirahooks.Lifecycle.Hydrate does) to find which parent issue this
// execution is for, then captures that issue plus every currently-known
// subtask as its own requirement source - mirroring Hydrate's real
// side effect so reconcile.go's downstream source-id lookup behaves
// identically to production.
type fakeJiraHydrator struct {
	store  *delivery.Store
	issues map[string]*fakeJiraIssue
}

func newFakeJiraHydrator(store *delivery.Store) *fakeJiraHydrator {
	return &fakeJiraHydrator{store: store, issues: map[string]*fakeJiraIssue{}}
}

func (f *fakeJiraHydrator) Issue(key string) *fakeJiraIssue {
	issue := &fakeJiraIssue{key: key}
	f.issues[key] = issue
	return issue
}

func (f *fakeJiraHydrator) Hydrate(ctx context.Context, executionID, sessionID, idempotencyKey string) ([]jirahooks.HydratedJiraSource, error) {
	execution, err := f.store.GetExecution(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("fakeJiraHydrator: get execution: %w", err)
	}
	lifecycle, err := f.store.GetDeliveryLifecycle(ctx, execution.OrchestrationID)
	if err != nil {
		return nil, fmt.Errorf("fakeJiraHydrator: get delivery lifecycle: %w", err)
	}
	parentKey := lifecycle.Case.JiraIssueKey
	issue, ok := f.issues[parentKey]
	if !ok {
		return nil, fmt.Errorf("fakeJiraHydrator: unknown issue %q", parentKey)
	}

	sources := []jirahooks.HydratedJiraSource{{IssueKey: parentKey, Title: "Parent " + parentKey}}
	for _, sub := range issue.subtasks {
		sources = append(sources, jirahooks.HydratedJiraSource{IssueKey: sub, ParentKey: parentKey, Title: "Subtask " + sub})
	}
	for _, src := range sources {
		if _, err := f.store.CaptureRequirement(ctx, idempotencyKey+":source:"+src.IssueKey, execution.OrchestrationID, delivery.SourceInput{
			Provider: "jira", ExternalID: src.IssueKey, ParentKey: src.ParentKey, Title: src.Title, Tenant: lifecycle.Case.SourceTenant,
		}); err != nil {
			return nil, fmt.Errorf("fakeJiraHydrator: capture requirement %s: %w", src.IssueKey, err)
		}
	}
	return sources, nil
}

// newServiceWithJira builds a Service wired to a fresh storage kernel and
// a fakeJiraHydrator sharing that same delivery.Store, matching
// production's WithJiraHydrator wiring.
func newServiceWithJira(t *testing.T) (*Service, *fakeJiraHydrator) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := delivery.NewStore(db)
	hydrator := newFakeJiraHydrator(store)
	return New(store, plan.NewStore(db), WithJiraHydrator(hydrator)), hydrator
}

func planDraft(title string) PlanDraft {
	return PlanDraft{Title: title}
}

func projectDraft(slug, repositoryURL, taskKey string) ProjectDraft {
	return ProjectDraft{
		Slug: slug, RepositoryURL: repositoryURL, DefaultBranch: "main",
		TaskKey: taskKey, Title: "Deliver " + taskKey,
		Plan: PlanDraft{Title: "Detailed plan for " + slug},
	}
}

func TestReconcileCreatesRunnableMultiProjectDelivery(t *testing.T) {
	svc, jira := newServiceWithJira(t)
	jira.Issue("ABC-123").WithSubtasks("ABC-124", "ABC-125")

	result := mustStart(t, svc, StartRequest{
		IdempotencyKey: "start-abc-123",
		Source:         &SourceIdentity{Kind: SourceJira, Provider: "jira", Tenant: "cloud-1", Key: "ABC-123"},
		Title:          "Checkout migration",
		HighLevelPlan:  planDraft("Move checkout to payments v2"),
		Projects: []ProjectDraft{
			projectDraft("checkout", "https://github.com/acme/checkout", "ABC-124"),
			projectDraft("payments", "https://github.com/acme/payments", "ABC-125"),
		},
	})

	if len(result.Reconciliation.Requirements) != 3 {
		t.Fatalf("Requirements = %v, want 3 (one parent, two subtasks)", result.Reconciliation.Requirements)
	}
	if len(result.Reconciliation.Projects) != 2 {
		t.Fatalf("Projects = %v, want 2", result.Reconciliation.Projects)
	}
	if len(result.Reconciliation.Plans) != 3 {
		t.Fatalf("Plans = %v, want 3 (one high-level, two project)", result.Reconciliation.Plans)
	}
	if len(result.Reconciliation.RunnableWork) != 2 {
		t.Fatalf("RunnableWork = %v, want 2 (independent lanes, nothing blocking either)", result.Reconciliation.RunnableWork)
	}

	view, err := svc.deliveries.BuildDeliveryView(context.Background(), result.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if len(view.Lanes) != 2 {
		t.Fatalf("view.Lanes = %+v, want 2 lanes", view.Lanes)
	}
	if len(view.ProjectPlans) != 2 {
		t.Fatalf("view.ProjectPlans = %+v, want 2 linked project plans", view.ProjectPlans)
	}
	if view.PlanID == "" || view.PlanRevision == 0 {
		t.Fatalf("view high-level plan = %q@%d, want it populated via LinkDeliveryPlan/OrchestrationDetails", view.PlanID, view.PlanRevision)
	}

	// A second call with the same Jira identity plus a newly discovered
	// third subtask/project must reconcile that new work onto the reused
	// execution, not short-circuit because nothing was freshly created at
	// the lifetime/execution level.
	jira.Issue("ABC-123").WithSubtasks("ABC-124", "ABC-125", "ABC-126")
	second := mustStart(t, svc, StartRequest{
		IdempotencyKey: "start-abc-123-round-2",
		Source:         &SourceIdentity{Kind: SourceJira, Provider: "jira", Tenant: "cloud-1", Key: "ABC-123"},
		Title:          "Checkout migration",
		HighLevelPlan:  planDraft("Move checkout to payments v2"),
		Projects: []ProjectDraft{
			projectDraft("checkout", "https://github.com/acme/checkout", "ABC-124"),
			projectDraft("payments", "https://github.com/acme/payments", "ABC-125"),
			projectDraft("billing", "https://github.com/acme/billing", "ABC-126"),
		},
	})
	if second.Execution.ID != result.Execution.ID {
		t.Fatalf("second Execution.ID = %q, want the same reused execution %q", second.Execution.ID, result.Execution.ID)
	}
	if len(second.Reconciliation.Requirements) != 4 {
		t.Fatalf("second Requirements = %v, want 4 (parent + three subtasks)", second.Reconciliation.Requirements)
	}
	if len(second.Reconciliation.Projects) != 3 {
		t.Fatalf("second Projects = %v, want 3", second.Reconciliation.Projects)
	}
	if len(second.Reconciliation.RunnableWork) != 3 {
		t.Fatalf("second RunnableWork = %v, want 3 (the new lane plus the two already-runnable ones)", second.Reconciliation.RunnableWork)
	}

	reView, err := svc.deliveries.BuildDeliveryView(context.Background(), result.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("BuildDeliveryView after second reconcile: %v", err)
	}
	if len(reView.Lanes) != 3 {
		t.Fatalf("view.Lanes after second reconcile = %+v, want 3", reView.Lanes)
	}
	if len(reView.ProjectPlans) != 3 {
		t.Fatalf("view.ProjectPlans after second reconcile = %+v, want 3", reView.ProjectPlans)
	}

	// The already-reconciled checkout/payments projects must not have been
	// duplicated or re-created under a different id on the second call.
	firstProjects := map[string]bool{}
	for _, id := range result.Reconciliation.Projects {
		firstProjects[id] = true
	}
	reusedCount := 0
	for _, id := range second.Reconciliation.Projects {
		if firstProjects[id] {
			reusedCount++
		}
	}
	if reusedCount != 2 {
		t.Fatalf("second call reused %d of the first call's 2 projects, want both reused unchanged", reusedCount)
	}
}

func TestReconcileWithoutJiraHydratorCapturesSuppliedRequirementDrafts(t *testing.T) {
	svc, err := (func() (*Service, error) {
		db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "adhoc.db"))
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = db.Close() })
		return New(delivery.NewStore(db), plan.NewStore(db)), nil
	})()
	if err != nil {
		t.Fatalf("build service: %v", err)
	}

	result := mustStart(t, svc, StartRequest{
		IdempotencyKey: "start-adhoc-reconcile",
		Source:         &SourceIdentity{Kind: SourceAdhoc},
		Title:          "One-off cleanup",
		Requirements: []RequirementDraft{
			{Provider: "github", ExternalID: "acme/infra#1", Title: "Cleanup ticket"},
		},
		Projects: []ProjectDraft{
			projectDraft("infra", "https://github.com/acme/infra", "acme/infra#1"),
		},
	})
	if len(result.Reconciliation.Requirements) != 1 {
		t.Fatalf("Requirements = %v, want 1", result.Reconciliation.Requirements)
	}
	if len(result.Reconciliation.Projects) != 1 {
		t.Fatalf("Projects = %v, want 1", result.Reconciliation.Projects)
	}
	if len(result.Reconciliation.RunnableWork) != 1 {
		t.Fatalf("RunnableWork = %v, want 1 (task_key matched by external id/canonical key)", result.Reconciliation.RunnableWork)
	}
}

func TestReconcileIsIdempotentOnExactRetry(t *testing.T) {
	svc, jira := newServiceWithJira(t)
	jira.Issue("XYZ-1").WithSubtasks("XYZ-2")

	req := StartRequest{
		IdempotencyKey: "start-xyz-1",
		Source:         &SourceIdentity{Kind: SourceJira, Provider: "jira", Tenant: "cloud-1", Key: "XYZ-1"},
		Title:          "Retry check",
		HighLevelPlan:  planDraft("Retry check plan"),
		Projects: []ProjectDraft{
			projectDraft("svc-a", "https://github.com/acme/svc-a", "XYZ-2"),
		},
	}
	first := mustStart(t, svc, req)
	second := mustStart(t, svc, req)

	if !strings.EqualFold(strings.Join(first.Reconciliation.Projects, ","), strings.Join(second.Reconciliation.Projects, ",")) {
		t.Fatalf("retry Projects = %v, want identical to first call %v", second.Reconciliation.Projects, first.Reconciliation.Projects)
	}
	if !strings.EqualFold(strings.Join(first.Reconciliation.Plans, ","), strings.Join(second.Reconciliation.Plans, ",")) {
		t.Fatalf("retry Plans = %v, want identical to first call %v (no needless revision bump)", second.Reconciliation.Plans, first.Reconciliation.Plans)
	}
}

// TestReconcileNamesRequirementsNoTaskCovers is the exact shape that went
// unnoticed: a Jira parent whose subtask was hydrated as a requirement
// source, returned in requirement_sources, and then left with no task and
// no lane - with an empty reconciliation.skipped, because the mapping
// only ever ran task-to-source and nothing looked the other way.
func TestReconcileNamesRequirementsNoTaskCovers(t *testing.T) {
	svc, jira := newServiceWithJira(t)
	jira.Issue("ABC-123").WithSubtasks("ABC-124")

	// The caller names no reference, so its one task covers the parent
	// key and the subtask is covered by nothing.
	result := mustStart(t, svc, StartRequest{
		IdempotencyKey: "start-uncovered",
		Source:         &SourceIdentity{Kind: SourceJira, Provider: "jira", Tenant: "cloud-1", Key: "ABC-123"},
		Projects: []ProjectDraft{{
			Slug: "checkout", RepositoryURL: "https://github.com/acme/checkout",
			Title: "the parent's own work", TaskKey: "ABC-123",
		}},
	})

	if len(result.Reconciliation.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want none - the task itself reconciled fine", result.Reconciliation.Skipped)
	}
	want := []string{"ABC-124"}
	if !slices.Equal(result.Reconciliation.UncoveredRequirements, want) {
		t.Fatalf("UncoveredRequirements = %v, want %v", result.Reconciliation.UncoveredRequirements, want)
	}
}

func TestReconcileNamesNothingUncoveredWhenEveryRequirementHasATask(t *testing.T) {
	svc, jira := newServiceWithJira(t)
	jira.Issue("ABC-123").WithSubtasks("ABC-124")

	result := mustStart(t, svc, StartRequest{
		IdempotencyKey: "start-covered",
		Source:         &SourceIdentity{Kind: SourceJira, Provider: "jira", Tenant: "cloud-1", Key: "ABC-123"},
		Projects: []ProjectDraft{
			{Slug: "checkout", RepositoryURL: "https://github.com/acme/checkout", Title: "parent work", TaskKey: "ABC-123"},
			{Slug: "checkout", RepositoryURL: "https://github.com/acme/checkout", Title: "subtask work", TaskKey: "ABC-124"},
		},
	})

	if len(result.Reconciliation.UncoveredRequirements) != 0 {
		t.Fatalf("UncoveredRequirements = %v, want none", result.Reconciliation.UncoveredRequirements)
	}
}
