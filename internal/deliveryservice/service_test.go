package deliveryservice

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
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
		Source: &SourceIdentity{
			Kind: SourceJira, Provider: "jira", Tenant: tenant, Key: key,
			Clarity: delivery.ClarityClear,
		},
		Title:         "Deliver " + key,
		HighLevelPlan: PlanDraft{Objective: "Deliver " + key},
		Session:       SessionStart{Participant: session},
	}
}

func adhocRequest(prompt, session string) StartRequest {
	return StartRequest{
		IdempotencyKey: "start-" + session,
		Source:         &SourceIdentity{Kind: SourceAdhoc},
		Title:          prompt,
		HighLevelPlan:  PlanDraft{Objective: prompt},
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

// The whole point of resolving the organisation before identity: a
// question must leave nothing behind. Hydration used to notice the wrong
// site two steps later, by which time the delivery already existed and
// its 404 was recorded as a skipped step rather than a wrong site.
func TestStartOrResolveWritesNothingWhenTheOrganisationIsStillAQuestion(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := delivery.NewStore(db)

	question := &protocol.NeedUserInput{
		Kind:     protocol.NeedUserInputKindDecisionRequired,
		Question: "Which Jira organisation holds PAY-1?",
		Options:  []protocol.NeedUserInputOptionsElem{{Id: "acme", Label: "acme", Impact: "starts there"}},
	}
	svc := New(store, plan.NewStore(db), WithJiraOrgResolver(
		func(context.Context, string, string) (string, *protocol.NeedUserInput, error) {
			return "", question, nil
		},
	))

	result, needsInput, err := svc.StartOrResolve(context.Background(), jiraRequest("", "PAY-1", "asker"))
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needsInput == nil || len(needsInput.Options) != 1 {
		t.Fatalf("needsInput = %+v, want the resolver's question", needsInput)
	}
	if result.Execution.OrchestrationID != "" {
		t.Fatalf("result = %+v, want nothing started", result)
	}

	var cases int
	if err := db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(1) FROM delivery_cases`).Scan(&cases); err != nil {
		t.Fatalf("count delivery_cases: %v", err)
	}
	if cases != 0 {
		t.Fatalf("delivery_cases = %d rows, want none written for an unanswered question", cases)
	}
}

// Once an issue has a lifetime it has already answered which site holds
// it; asking again risks a second delivery for one issue.
func TestStartOrResolveReusesTheOrganisationAnActiveLifetimeAlreadyNames(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := delivery.NewStore(db)

	asked := 0
	resolver := WithJiraOrgResolver(func(_ context.Context, named, _ string) (string, *protocol.NeedUserInput, error) {
		if named == "" {
			asked++
			return "acme", nil, nil
		}
		return named, nil, nil
	})
	svc := New(store, plan.NewStore(db), resolver)

	if _, needsInput, err := svc.StartOrResolve(context.Background(), jiraRequest("", "PAY-1", "first")); err != nil || needsInput != nil {
		t.Fatalf("first start: needsInput=%v err=%v", needsInput, err)
	}
	if _, needsInput, err := svc.StartOrResolve(context.Background(), jiraRequest("", "PAY-1", "second")); err != nil || needsInput != nil {
		t.Fatalf("second start: needsInput=%v err=%v", needsInput, err)
	}
	if asked != 1 {
		t.Fatalf("resolver saw a blank organisation %d times, want once - the second start reads what the first recorded", asked)
	}
}

// A delivery is the execution of a plan, and one started without a plan
// has nothing a later session can resume against - but that is worth
// saying, not worth standing in the way of the work. It warns, opens the
// delivery, and leaves a gap behind; naming a plan nobody saved is the
// same answer rather than a pointer to nothing.
func TestStartOrResolveWarnsButStartsADeliveryWithNoPlan(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := delivery.NewStore(db)
	svc := New(store, plan.NewStore(db))

	req := jiraRequest("acme", "PAY-1", "planless")
	req.HighLevelPlan = PlanDraft{}

	result, needsInput, err := svc.StartOrResolve(context.Background(), req)
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needsInput != nil {
		t.Fatalf("needsInput = %+v, want none - a missing plan does not block the work", needsInput)
	}
	if result.Execution.OrchestrationID == "" {
		t.Fatal("no delivery was opened; a missing plan is a warning, not a refusal")
	}
	if len(result.Reconciliation.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want exactly the missing-plan warning", result.Reconciliation.Warnings)
	}
	view, err := store.BuildDeliveryView(context.Background(), result.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	readiness := delivery.AssessCompletionReadiness(view)
	if !slices.ContainsFunc(readiness.Gaps, func(gap delivery.ReadinessGap) bool { return gap.Code == delivery.GapPlanMissing }) {
		t.Fatalf("gaps = %+v, want %s so completing without a plan still says so", readiness.Gaps, delivery.GapPlanMissing)
	}

	// Naming a plan that was never saved points the delivery at nothing,
	// which is worse than pointing it at none: the reference is dropped
	// and reported.
	unsaved := jiraRequest("acme", "PAY-2", "unsaved plan")
	unsaved.HighLevelPlan = PlanDraft{}
	unsaved.PlanID, unsaved.PlanRevision = "PLAN-NOT-SAVED", 3
	result, needsInput, err = svc.StartOrResolve(context.Background(), unsaved)
	if err != nil {
		t.Fatalf("StartOrResolve with an unsaved plan: %v", err)
	}
	if needsInput != nil {
		t.Fatalf("needsInput = %+v, want none", needsInput)
	}
	if len(result.Reconciliation.Warnings) != 1 || !strings.Contains(result.Reconciliation.Warnings[0], "PLAN-NOT-SAVED") {
		t.Fatalf("Warnings = %v, want one naming the plan nobody saved", result.Reconciliation.Warnings)
	}
	dropped, err := store.BuildDeliveryView(context.Background(), result.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("BuildDeliveryView: %v", err)
	}
	if dropped.PlanID != "" {
		t.Fatalf("delivery kept plan reference %q, want it dropped rather than dangling", dropped.PlanID)
	}
}

// The plan a delivery executes has to be able to change: the requirement
// moves, or the first plan turns out to be wrong.
func TestStartOrResolveMovesTheDeliveryOntoARevisedPlan(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := delivery.NewStore(db)
	svc := New(store, plan.NewStore(db))
	ctx := context.Background()

	first, needsInput, err := svc.StartOrResolve(ctx, jiraRequest("acme", "PAY-1", "first"))
	if err != nil || needsInput != nil {
		t.Fatalf("first start: needsInput=%v err=%v", needsInput, err)
	}
	orchestrationID := first.Execution.OrchestrationID

	before, err := store.GetOrchestration(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("GetOrchestration: %v", err)
	}
	if before.PlanId == nil || before.PlanRevision == nil || *before.PlanRevision != 1 {
		t.Fatalf("orchestration plan = %v r%v, want the first revision", before.PlanId, before.PlanRevision)
	}

	revised := jiraRequest("acme", "PAY-1", "second")
	revised.HighLevelPlan = PlanDraft{Objective: "Deliver PAY-1 the other way", ReasonForChange: "the requirement moved"}
	if _, needsInput, err := svc.StartOrResolve(ctx, revised); err != nil || needsInput != nil {
		t.Fatalf("second start: needsInput=%v err=%v", needsInput, err)
	}

	after, err := store.GetOrchestration(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("GetOrchestration after revising: %v", err)
	}
	if after.PlanRevision == nil || *after.PlanRevision != 2 || *after.PlanId != *before.PlanId {
		t.Fatalf("orchestration plan = %v r%v, want the same lineage at revision 2", after.PlanId, after.PlanRevision)
	}

	// Linked, not merely named: a plan the delivery points at but never
	// linked left the panel's Plans tab empty for a delivery that had one.
	var links int
	if err := db.Reader().QueryRowContext(ctx, `SELECT COUNT(1) FROM delivery_plan_links WHERE orchestration_id = ? AND scope = 'delivery'`, orchestrationID).Scan(&links); err != nil {
		t.Fatalf("count delivery_plan_links: %v", err)
	}
	if links != 2 {
		t.Fatalf("delivery plan links = %d, want one per revision", links)
	}
}

// Stating clarity was required for a while, which made a one-line fix
// carry the same ceremony as a vague epic. It is a warning now: an
// unstated judgement is reported and nothing is guessed, while a stated
// one that is neither legal value is still caught.
func TestStartOrResolveWarnsAboutAnUnstatedClarity(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	unjudged := jiraRequest("acme", "PAY-1", "unjudged")
	unjudged.Source.Clarity = ""
	result, needsInput, err := svc.StartOrResolve(ctx, unjudged)
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needsInput != nil {
		t.Fatalf("needsInput = %+v, want none - a trivial task owes nobody an assessment", needsInput)
	}
	if result.Execution.OrchestrationID == "" {
		t.Fatal("no delivery was opened for an unjudged issue")
	}
	if len(result.Reconciliation.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want exactly the unstated-clarity warning", result.Reconciliation.Warnings)
	}

	// A clarity that is neither legal value is a typo, not a judgement.
	typo := jiraRequest("acme", "PAY-2", "typo")
	typo.Source.Clarity = "clearish"
	_, needsInput, err = svc.StartOrResolve(ctx, typo)
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needsInput == nil || !slices.Contains(needsInput.MissingFields, "source.clarity") {
		t.Fatalf("needsInput = %+v, want a question naming source.clarity", needsInput)
	}

	// Unclear without saying what is unclear leaves nobody anything to
	// answer, so that one is still a question.
	silent := jiraRequest("acme", "PAY-3", "silent")
	silent.Source.Clarity = delivery.ClarityNeedsClarification
	_, needsInput, err = svc.StartOrResolve(ctx, silent)
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needsInput == nil || !slices.Contains(needsInput.MissingFields, "source.clarity_rationale") {
		t.Fatalf("needsInput = %+v, want a question naming source.clarity_rationale", needsInput)
	}

	// An adhoc delivery has no Jira issue to judge and is unaffected.
	if _, needsInput, err := svc.StartOrResolve(ctx, adhocRequest("one-off cleanup", "adhoc")); err != nil || needsInput != nil {
		t.Fatalf("adhoc start: needsInput=%v err=%v", needsInput, err)
	}
}

// The clarity a start states is recorded as the delivery's own
// assessment, through the same call assess_jira_delivery makes, and a
// retried start records one rather than failing on the one it wrote.
func TestStartOrResolveRecordsTheStatedClarityOnce(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := delivery.NewStore(db)
	svc := New(store, plan.NewStore(db))
	ctx := context.Background()

	req := jiraRequest("acme", "PAY-1", "judge")
	req.Source.Clarity = delivery.ClarityNeedsClarification
	req.Source.ClarityRationale = "The acceptance criteria name no API"

	start, needsInput, err := svc.StartOrResolve(ctx, req)
	if err != nil || needsInput != nil {
		t.Fatalf("StartOrResolve: needsInput=%v err=%v", needsInput, err)
	}
	if _, _, err := svc.StartOrResolve(ctx, req); err != nil {
		t.Fatalf("retried StartOrResolve: %v", err)
	}

	lifecycle, err := store.GetDeliveryLifecycle(ctx, start.Execution.OrchestrationID)
	if err != nil {
		t.Fatalf("GetDeliveryLifecycle: %v", err)
	}
	if len(lifecycle.Assessments) != 1 {
		t.Fatalf("assessments = %d, want exactly one for a start and its retry", len(lifecycle.Assessments))
	}
	got := lifecycle.Assessments[0]
	if got.Clarity != delivery.ClarityNeedsClarification || got.Rationale != req.Source.ClarityRationale {
		t.Fatalf("assessment = %+v, want the clarity and rationale the start stated", got)
	}
}
