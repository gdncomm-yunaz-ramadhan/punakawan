package jiraintegration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/providerwrite"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeAdapterCaller records every "execute" call it receives and answers
// with a canned response per op, instead of talking to a real spawned
// adapter process.
type fakeAdapterCaller struct {
	calls     []map[string]any
	responses map[string]string // op -> raw JSON response body
	failOps   map[string]bool
}

func (f *fakeAdapterCaller) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args, _ := params.(map[string]any)
	f.calls = append(f.calls, args)
	op, _ := args["op"].(string)
	if f.failOps[op] {
		return nil, fmt.Errorf("simulated adapter failure for %q", op)
	}
	if resp, ok := f.responses[op]; ok {
		return json.RawMessage(resp), nil
	}
	return json.RawMessage(`{"ok":true}`), nil
}

var permissiveInputSchema = protocol.AdapterManifestOperationsValueInputSchema{"type": "object"}

func testManifest() protocol.AdapterManifest {
	return protocol.AdapterManifest{
		Id: "atlassian", Name: "atlassian", Version: "0.1.0", Protocol: "punakawan.adapter/v1",
		Runtime: protocol.AdapterManifestRuntimeNode,
		Permissions: protocol.AdapterManifestPermissions{
			Network:    protocol.AdapterManifestPermissionsNetwork{Hosts: []string{"api.atlassian.com"}},
			Filesystem: protocol.AdapterManifestPermissionsFilesystem{Read: []string{}, Write: []string{}},
			Secrets:    []string{},
		},
		Operations: protocol.AdapterManifestOperations{
			"atlassian.getJiraIssue":               {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.getJiraComments":            {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.getTransitionsForJiraIssue": {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.addJiraComment":             {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.addWorklog":                 {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.transitionJiraIssue":        {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
		},
	}
}

type fakeGateResolver struct {
	gate *adapters.Gate
	// asked records every adapter id resolved, so a test can assert which
	// organisation a write was routed to.
	asked []string
}

func (f *fakeGateResolver) Gate(ctx context.Context, adapterID string) (*adapters.Gate, error) {
	f.asked = append(f.asked, adapterID)
	return f.gate, nil
}

// newTestService builds a Service wired to a fake adapter caller, a fresh
// delivery.Store/storage kernel, and a durable outbox.
func newTestService(t *testing.T, cfg *jiraworkflow.Config) (svc *Service, store *delivery.Store, ob *outbox.Store, fc *fakeAdapterCaller) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	fc = &fakeAdapterCaller{}
	gate := adapters.NewGate("atlassian", testManifest(), fc)
	store = delivery.NewStore(db)
	ob = outbox.New(db)
	svc = NewService(store, &fakeGateResolver{gate: gate}, ob, cfg)
	return svc, store, ob, fc
}

// drainOutbox runs a Worker against store until it reports no more
// claimable work, so a test can observe the effect of every intent a
// Service call enqueued (Service itself never executes a write).
func drainOutbox(t *testing.T, store *outbox.Store, registry GateResolver) {
	t.Helper()
	worker := &providerwrite.Worker{ID: "test-worker", Store: store, Adapters: registry}
	for i := 0; i < 20; i++ {
		did, err := worker.RunOnce(context.Background())
		if err != nil {
			t.Fatalf("drainOutbox: RunOnce: %v", err)
		}
		if !did {
			return
		}
	}
	t.Fatal("drainOutbox: too many iterations; an intent may be stuck retrying forever in this test")
}

func captureJiraRequirement(t *testing.T, store *delivery.Store, orchestrationID, issueKey string) {
	t.Helper()
	if _, err := store.CaptureRequirement(context.Background(), "cap-"+delivery.NewID(), orchestrationID, delivery.SourceInput{
		Provider: "jira", ExternalID: issueKey, Title: "Refund API",
	}); err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
}

func TestOnDeliveryStarted_AutoLogOffIsANoOp(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: false, CommentEvents: []string{"delivery.started"}}
	svc, store, ob, fc := newTestService(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := svc.OnDeliveryStarted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryStarted: %v", err)
	}
	drainOutbox(t, ob, svc.registry)
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none when auto_log is off", fc.calls)
	}
}

func TestOnDeliveryStarted_NoLinkedIssueSkipsSilently(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	svc, store, ob, fc := newTestService(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	if err := svc.OnDeliveryStarted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryStarted: %v", err)
	}
	drainOutbox(t, ob, svc.registry)
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none when no Jira issue is linked", fc.calls)
	}
}

func TestOnDeliveryStarted_PostsCommentAndDedupesOnRetry(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	svc, store, ob, fc := newTestService(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := svc.OnDeliveryStarted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryStarted: %v", err)
	}
	// A retried call for the same delivery must not enqueue a second intent.
	if err := svc.OnDeliveryStarted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryStarted (retry): %v", err)
	}
	drainOutbox(t, ob, svc.registry)

	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.addJiraComment" {
		t.Fatalf("calls = %+v, want exactly one addJiraComment call", fc.calls)
	}
	if fc.calls[0]["issueIdOrKey"] != "PAY-1" {
		t.Fatalf("issueIdOrKey = %v, want PAY-1", fc.calls[0]["issueIdOrKey"])
	}
	body, _ := fc.calls[0]["commentBody"].(string)
	if !strings.Contains(body, "delivery started") || !strings.Contains(body, orch.Id) {
		t.Errorf("commentBody = %q, want it to describe the delivery", body)
	}
}

func TestOnDeliveryStarted_EnqueuesConfiguredStartTransition(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, Transitions: map[string]jiraworkflow.TransitionPolicy{
		"PAY": {StartStatus: "In Progress"},
	}}
	svc, store, ob, fc := newTestService(t, cfg)
	fc.responses = map[string]string{
		"atlassian.getJiraIssue":               `{"normalized":{"key":"PAY-1","status":"To Do"}}`,
		"atlassian.getTransitionsForJiraIssue": `{"transitions":[{"id":"11","name":"Start","toStatus":{"id":"3","name":"In Progress"}}]}`,
	}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := svc.OnDeliveryStarted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryStarted: %v", err)
	}
	drainOutbox(t, ob, svc.registry)

	var sawTransition bool
	for _, c := range fc.calls {
		if c["op"] == "atlassian.transitionJiraIssue" {
			sawTransition = true
			if c["transitionId"] != "11" {
				t.Errorf("transitionId = %v, want 11", c["transitionId"])
			}
		}
	}
	if !sawTransition {
		t.Fatalf("calls = %+v, want a transitionJiraIssue call", fc.calls)
	}
}

func TestOnDeliveryStarted_NoPolicyNeverAttemptsATransition(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true}
	svc, store, ob, fc := newTestService(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := svc.OnDeliveryStarted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryStarted: %v", err)
	}
	drainOutbox(t, ob, svc.registry)
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none with no configured policy and no comment_events", fc.calls)
	}
}

func TestOnDeliveryCompleted_TransitionOnCompleteFallsBackToDoneWithNoPolicy(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, TransitionOnComplete: true}
	svc, store, ob, fc := newTestService(t, cfg)
	fc.responses = map[string]string{
		"atlassian.getJiraIssue":               `{"normalized":{"key":"PAY-1","status":"In Progress"}}`,
		"atlassian.getTransitionsForJiraIssue": `{"transitions":[{"id":"31","name":"Close","toStatus":{"id":"3","name":"Done"}}]}`,
	}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := svc.OnDeliveryCompleted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryCompleted: %v", err)
	}
	drainOutbox(t, ob, svc.registry)

	var sawTransition bool
	for _, c := range fc.calls {
		if c["op"] == "atlassian.transitionJiraIssue" {
			sawTransition = true
			if c["transitionId"] != "31" {
				t.Errorf("transitionId = %v, want 31", c["transitionId"])
			}
		}
	}
	if !sawTransition {
		t.Fatalf("calls = %+v, want a transitionJiraIssue call to the default Done status", fc.calls)
	}
}

func TestOnDeliveryCompleted_UsesConfiguredCompleteStatusOverDefault(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, TransitionOnComplete: true, Transitions: map[string]jiraworkflow.TransitionPolicy{
		"PAY": {CompleteStatus: "Shipped"},
	}}
	svc, store, ob, fc := newTestService(t, cfg)
	fc.responses = map[string]string{
		"atlassian.getJiraIssue":               `{"normalized":{"key":"PAY-1","status":"In Progress"}}`,
		"atlassian.getTransitionsForJiraIssue": `{"transitions":[{"id":"41","name":"Ship","toStatus":{"id":"5","name":"Shipped"}}]}`,
	}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := svc.OnDeliveryCompleted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryCompleted: %v", err)
	}
	drainOutbox(t, ob, svc.registry)

	var sawTransition bool
	for _, c := range fc.calls {
		if c["op"] == "atlassian.transitionJiraIssue" {
			sawTransition = true
			if c["transitionId"] != "41" {
				t.Errorf("transitionId = %v, want 41", c["transitionId"])
			}
		}
	}
	if !sawTransition {
		t.Fatalf("calls = %+v, want a transitionJiraIssue call to the configured Shipped status", fc.calls)
	}
}

func TestOnDeliveryCompleted_TransitionOnCompleteFalseNeverAttemptsTransition(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, TransitionOnComplete: false}
	svc, store, ob, fc := newTestService(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := svc.OnDeliveryCompleted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryCompleted: %v", err)
	}
	drainOutbox(t, ob, svc.registry)
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none with transition_on_complete false and no comment_events", fc.calls)
	}
}

func TestOnDeliveryCompleted_ZeroMatchesIsAConfigurationError(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, TransitionOnComplete: true}
	svc, store, _, fc := newTestService(t, cfg)
	fc.responses = map[string]string{
		"atlassian.getJiraIssue":               `{"normalized":{"key":"PAY-1","status":"In Progress"}}`,
		"atlassian.getTransitionsForJiraIssue": `{"transitions":[{"id":"31","name":"Reopen","toStatus":{"id":"3","name":"To Do"}}]}`,
	}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	err = svc.OnDeliveryCompleted(ctx, orch.Id)
	if err == nil {
		t.Fatal("expected an error when no transition reaches the configured Done status")
	}
	if !errors.Is(err, ErrTransitionNotConfigured) {
		t.Fatalf("error = %v, want it to wrap ErrTransitionNotConfigured", err)
	}
}

func TestOnDeliveryCompleted_MultipleMatchesReturnsAmbiguous(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, TransitionOnComplete: true}
	svc, store, _, fc := newTestService(t, cfg)
	fc.responses = map[string]string{
		"atlassian.getJiraIssue": `{"normalized":{"key":"PAY-1","status":"In Progress"}}`,
		"atlassian.getTransitionsForJiraIssue": `{"transitions":[
			{"id":"31","name":"Close as fixed","toStatus":{"id":"3","name":"Done"}},
			{"id":"32","name":"Done","toStatus":{"id":"4","name":"Closed"}}
		]}`,
	}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	err = svc.OnDeliveryCompleted(ctx, orch.Id)
	if err == nil {
		t.Fatal("expected an ambiguous-transition error")
	}
	var ambiguous *ErrTransitionAmbiguous
	if !errors.As(err, &ambiguous) {
		t.Fatalf("error = %v, want *ErrTransitionAmbiguous", err)
	}
	if len(ambiguous.Options) != 2 {
		t.Fatalf("Options = %+v, want 2 finite options", ambiguous.Options)
	}
	input := ambiguous.NeedUserInput()
	if input.Kind != protocol.NeedUserInputKindDecisionRequired {
		t.Errorf("Kind = %v, want decision_required", input.Kind)
	}
	if len(input.Options) != 2 {
		t.Errorf("NeedUserInput options = %+v, want 2", input.Options)
	}
}

func TestOnDeliveryCompleted_AlreadyAtTargetStatusEnqueuesNothing(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, TransitionOnComplete: true}
	svc, store, ob, fc := newTestService(t, cfg)
	fc.responses = map[string]string{
		"atlassian.getJiraIssue": `{"normalized":{"key":"PAY-1","status":"Done"}}`,
	}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := svc.OnDeliveryCompleted(ctx, orch.Id); err != nil {
		t.Fatalf("OnDeliveryCompleted: %v", err)
	}
	drainOutbox(t, ob, svc.registry)
	for _, c := range fc.calls {
		if c["op"] == "atlassian.transitionJiraIssue" {
			t.Fatalf("expected no transition call when the issue is already at the target status, got %+v", fc.calls)
		}
	}
}

func TestOnWorkRecorded_ProjectsExplicitWorklogToExactJiraTask(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, LogWork: true}
	svc, store, ob, fc := newTestService(t, cfg)
	fc.responses = map[string]string{"atlassian.addWorklog": `{"ok":true,"worklogId":"jira-worklog-1"}`}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-worklog", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	project, err := store.RegisterProject(ctx, "project-worklog", delivery.NewID(), "worklog-project", "https://example.test/worklog.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	lane, err := store.CreateLane(ctx, "lane-worklog", delivery.NewID(), orch.Id, project.Id, "")
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	entry, err := store.RecordWorkLog(ctx, "ledger-worklog", "worklog-1", orch.Id, lane.Id, "session-1", "PAY-1901", time.Now().UTC(), 300, "Implemented retry policy")
	if err != nil {
		t.Fatalf("RecordWorkLog: %v", err)
	}

	if err := svc.OnWorkRecorded(ctx, entry.ID); err != nil {
		t.Fatalf("OnWorkRecorded: %v", err)
	}
	// A retried call for the same immutable interval must not enqueue a
	// second intent.
	if err := svc.OnWorkRecorded(ctx, entry.ID); err != nil {
		t.Fatalf("OnWorkRecorded (retry): %v", err)
	}
	drainOutbox(t, ob, svc.registry)

	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.addWorklog" {
		t.Fatalf("calls = %+v, want exactly one addWorklog", fc.calls)
	}
	if fc.calls[0]["issueIdOrKey"] != "PAY-1901" || fc.calls[0]["timeSpentSeconds"] != 300 {
		t.Fatalf("worklog call = %+v, want exact task and duration", fc.calls[0])
	}
}

func TestOnWorkRecorded_LogWorkOffIsANoOp(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, LogWork: false}
	svc, store, ob, fc := newTestService(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-worklog", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	project, err := store.RegisterProject(ctx, "project-worklog", delivery.NewID(), "worklog-project", "https://example.test/worklog.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	lane, err := store.CreateLane(ctx, "lane-worklog", delivery.NewID(), orch.Id, project.Id, "")
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	entry, err := store.RecordWorkLog(ctx, "ledger-worklog", "worklog-1", orch.Id, lane.Id, "session-1", "PAY-1901", time.Now().UTC(), 300, "Implemented retry policy")
	if err != nil {
		t.Fatalf("RecordWorkLog: %v", err)
	}

	if err := svc.OnWorkRecorded(ctx, entry.ID); err != nil {
		t.Fatalf("OnWorkRecorded: %v", err)
	}
	drainOutbox(t, ob, svc.registry)
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none with log_work off", fc.calls)
	}
}

func TestReconcileIntent_DispatchesByOperation(t *testing.T) {
	svc, _, _, fc := newTestService(t, &jiraworkflow.Config{})
	fc.responses = map[string]string{
		"atlassian.getJiraComments": `{"comments":[{"id":"c-1","body":"punakawan:intent:intent-1"}]}`,
	}
	payload, _ := json.Marshal(map[string]any{"comment_body": "hello"})
	intent := outbox.Intent{ID: "intent-1", AdapterID: "atlassian", Operation: "atlassian.addJiraComment", TargetKey: "PAY-1", PayloadJSON: string(payload)}

	result, err := svc.ReconcileIntent(context.Background(), intent)
	if err != nil {
		t.Fatalf("ReconcileIntent: %v", err)
	}
	if result.State != providerwrite.ReconcileApplied || result.ExternalID != "c-1" {
		t.Fatalf("result = %+v, want ReconcileApplied with c-1", result)
	}
}

func TestReconcileIntent_UnregisteredOperationIsUnknown(t *testing.T) {
	svc, _, _, _ := newTestService(t, &jiraworkflow.Config{})
	intent := outbox.Intent{ID: "intent-1", AdapterID: "atlassian", Operation: "atlassian.editJiraIssue", TargetKey: "PAY-1", PayloadJSON: "{}"}

	result, err := svc.ReconcileIntent(context.Background(), intent)
	if err != nil {
		t.Fatalf("ReconcileIntent: %v", err)
	}
	if result.State != providerwrite.ReconcileUnknown {
		t.Fatalf("state = %v, want ReconcileUnknown for an operation with no registered reconciler", result.State)
	}
}

func TestPostComment_PostsAndDedupesOnRetry(t *testing.T) {
	svc, store, _, fc := newTestService(t, nil)
	fc.responses = map[string]string{"atlassian.addJiraComment": `{"ok":true,"commentId":"c-1"}`}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	first, err := svc.PostComment(ctx, orch.Id, "PAY-1", "hello there", "idem-1")
	if err != nil {
		t.Fatalf("PostComment: %v", err)
	}
	if first.Status != outbox.StatusSucceeded || first.ExternalID != "c-1" {
		t.Fatalf("first = %+v, want succeeded with external id c-1", first)
	}
	// A retry with the same idempotency key must not enqueue a second
	// intent, and must resolve to the same one already recorded.
	second, err := svc.PostComment(ctx, orch.Id, "PAY-1", "hello there", "idem-1")
	if err != nil {
		t.Fatalf("PostComment (retry): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("retry resolved to a different intent: first=%s second=%s", first.ID, second.ID)
	}

	var commentCalls int
	for _, c := range fc.calls {
		if c["op"] == "atlassian.addJiraComment" {
			commentCalls++
		}
	}
	if commentCalls != 1 {
		t.Fatalf("calls = %+v, want exactly one addJiraComment call", fc.calls)
	}
}

func TestPostComment_DifferentIdempotencyKeysAreDistinctCalls(t *testing.T) {
	svc, store, _, fc := newTestService(t, nil)
	fc.responses = map[string]string{"atlassian.addJiraComment": `{"ok":true,"commentId":"c-1"}`}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if _, err := svc.PostComment(ctx, orch.Id, "PAY-1", "first comment", "idem-1"); err != nil {
		t.Fatalf("PostComment (idem-1): %v", err)
	}
	if _, err := svc.PostComment(ctx, orch.Id, "PAY-1", "second comment", "idem-2"); err != nil {
		t.Fatalf("PostComment (idem-2): %v", err)
	}

	var commentCalls int
	for _, c := range fc.calls {
		if c["op"] == "atlassian.addJiraComment" {
			commentCalls++
		}
	}
	if commentCalls != 2 {
		t.Fatalf("calls = %+v, want two distinct addJiraComment calls for two different idempotency keys", fc.calls)
	}
}

// TestPostComment_RoutesToTheOrganisationTheDeliveryBelongsTo mirrors
// TestWritesRouteToTheOrganisationTheDeliveryBelongsTo for the explicit,
// agent-directed PostComment path: it must resolve the adapter the same
// per-organisation way every other Jira write does.
func TestPostComment_RoutesToTheOrganisationTheDeliveryBelongsTo(t *testing.T) {
	svc, store, _, fc := newTestService(t, nil)
	fc.responses = map[string]string{"atlassian.addJiraComment": `{"ok":true,"commentId":"c-1"}`}
	ctx := context.Background()

	resolved, err := store.StartOrResolveExecution(ctx, "start-1", delivery.SourceIdentity{
		Kind: delivery.SourceKindJira, Provider: "jira", Tenant: "gdncomm", Key: "PAY-1",
	}, delivery.OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	deliveryID := resolved.Execution.OrchestrationID
	captureJiraRequirement(t, store, deliveryID, "PAY-1")

	if _, err := svc.PostComment(ctx, deliveryID, "PAY-1", "hello there", "idem-1"); err != nil {
		t.Fatalf("PostComment: %v", err)
	}

	asked := svc.registry.(*fakeGateResolver).asked
	if len(asked) == 0 {
		t.Fatal("expected PostComment to resolve an adapter")
	}
	for _, adapterID := range asked {
		if adapterID != "atlassian:gdncomm" {
			t.Errorf("write routed to %q, want atlassian:gdncomm", adapterID)
		}
	}
}

// TestWritesRouteToTheOrganisationTheDeliveryBelongsTo is the whole point
// of per-organisation adapter ids: two Jira sites are two credentials, so
// a write for one must never be handed to the process holding the
// other's token.
func TestWritesRouteToTheOrganisationTheDeliveryBelongsTo(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	svc, store, ob, _ := newTestService(t, cfg)
	ctx := context.Background()

	resolved, err := store.StartOrResolveExecution(ctx, "start-1", delivery.SourceIdentity{
		Kind: delivery.SourceKindJira, Provider: "jira", Tenant: "gdncomm", Key: "PAY-1",
	}, delivery.OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	deliveryID := resolved.Execution.OrchestrationID
	captureJiraRequirement(t, store, deliveryID, "PAY-1")

	if err := svc.OnDeliveryStarted(ctx, deliveryID); err != nil {
		t.Fatalf("OnDeliveryStarted: %v", err)
	}
	drainOutbox(t, ob, svc.registry)

	asked := svc.registry.(*fakeGateResolver).asked
	if len(asked) == 0 {
		t.Fatal("expected the queued Jira write to resolve an adapter")
	}
	for _, adapterID := range asked {
		if adapterID != "atlassian:gdncomm" {
			t.Errorf("write routed to %q, want atlassian:gdncomm", adapterID)
		}
	}
}
