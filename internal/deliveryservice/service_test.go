package deliveryservice

import (
	"context"
	"path/filepath"
	"slices"
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

// A delivery is the execution of a plan. Starting one without a plan used
// to be accepted silently, and afterwards looked exactly like a delivery
// whose plan had simply not been linked.
func TestStartOrResolveRefusesToOpenADeliveryWithNoPlan(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(delivery.NewStore(db), plan.NewStore(db))

	req := jiraRequest("acme", "PAY-1", "planless")
	req.HighLevelPlan = PlanDraft{}

	result, needsInput, err := svc.StartOrResolve(context.Background(), req)
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needsInput == nil || len(needsInput.MissingFields) != 1 || needsInput.MissingFields[0] != "plan" {
		t.Fatalf("needsInput = %+v, want a question naming plan", needsInput)
	}
	if result.Execution.OrchestrationID != "" {
		t.Fatalf("result = %+v, want nothing started", result)
	}

	var cases int
	if err := db.Reader().QueryRowContext(context.Background(), `SELECT COUNT(1) FROM delivery_cases`).Scan(&cases); err != nil {
		t.Fatalf("count delivery_cases: %v", err)
	}
	if cases != 0 {
		t.Fatalf("delivery_cases = %d rows, want none written for a delivery that was never opened", cases)
	}

	// Naming a plan that was never saved is the same answer: "has a plan"
	// used to be satisfiable with any two values, since nothing looked it
	// up.
	req.PlanID, req.PlanRevision = "PLAN-NOT-SAVED", 3
	_, needsInput, err = svc.StartOrResolve(context.Background(), req)
	if err != nil {
		t.Fatalf("StartOrResolve with an unsaved plan: %v", err)
	}
	if needsInput == nil || len(needsInput.MissingFields) != 1 || needsInput.MissingFields[0] != "plan_id" {
		t.Fatalf("needsInput = %+v, want a question naming plan_id", needsInput)
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

// A requirement nobody judged is what produces a delivery built on a
// guess, so the judgement is asked for at the point work is opened - not
// left to a separate tool an agent may never call.
func TestStartOrResolveRequiresAStatedClarityForAJiraSource(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	unjudged := jiraRequest("acme", "PAY-1", "unjudged")
	unjudged.Source.Clarity = ""
	_, needsInput, err := svc.StartOrResolve(ctx, unjudged)
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needsInput == nil || !slices.Contains(needsInput.MissingFields, "source.clarity") {
		t.Fatalf("needsInput = %+v, want a question naming source.clarity", needsInput)
	}

	// Unclear without saying what is unclear leaves nobody anything to
	// answer, so it is the same refusal.
	silent := jiraRequest("acme", "PAY-1", "silent")
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
