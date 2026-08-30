package jirahooks

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/deliveryhooks"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/providerwrite"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeAdapterCaller records every "execute" call it receives and answers
// with a canned response per op, instead of talking to a real spawned
// adapter process - the same fake-caller pattern
// internal/adapters/gate_test.go and tools_jiraprogress_test.go use.
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

// permissiveInputSchema is just enough of an input_schema to satisfy Gate's
// per-call payload validation without constraining which params a test may
// pass - these tests exercise JiraHook/Lifecycle behavior, not adapter
// schema strictness.
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
			"atlassian.getTransitionsForJiraIssue":  {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.addJiraComment":              {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.addWorklog":                  {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.transitionJiraIssue":         {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
		},
	}
}

// fakeGateResolver hands back a pre-built Gate instead of spawning a real
// adapter subprocess, satisfying JiraHook's gateResolver dependency.
type fakeGateResolver struct{ gate *adapters.Gate }

func (f *fakeGateResolver) Gate(ctx context.Context, adapterID string) (*adapters.Gate, error) {
	return f.gate, nil
}

// drainOutbox runs a Worker against store until it reports no more claimable
// work, so a test can observe the effect of every intent a Handle call
// enqueued (Handle itself never executes a write - see jirahooks.go's
// package doc comment). observer may be nil.
func drainOutbox(t *testing.T, store *outbox.Store, registry providerwriteGateResolver, observer providerwrite.SuccessObserver) {
	t.Helper()
	worker := &providerwrite.Worker{ID: "test-worker", Store: store, Adapters: registry, Observer: observer}
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

// providerwriteGateResolver is the exact method set providerwrite.Worker
// needs; *fakeGateResolver already satisfies it.
type providerwriteGateResolver interface {
	Gate(ctx context.Context, adapterID string) (*adapters.Gate, error)
}

// newTestHook builds a JiraHook wired to a fake adapter caller, a fresh
// delivery.Store/storage kernel, and a durable outbox. Returns the hook, the
// underlying delivery.Store (for capturing requirements), the outbox store
// (for draining), and the fake caller (for asserting on calls made).
func newTestHook(t *testing.T, cfg *jiraworkflow.Config) (hook *JiraHook, store *delivery.Store, ob *outbox.Store, fc *fakeAdapterCaller) {
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
	hook = NewJiraHook(db, store, nil, ob, cfg)
	hook.registry = &fakeGateResolver{gate: gate}
	return hook, store, ob, fc
}

// captureJiraRequirement seeds orchestrationID with a Jira-sourced
// requirement so JiraHook.resolveIssueKey finds issueKey.
func captureJiraRequirement(t *testing.T, store *delivery.Store, orchestrationID, issueKey string) {
	t.Helper()
	if _, err := store.CaptureRequirement(context.Background(), "cap-"+delivery.NewID(), orchestrationID, delivery.SourceInput{
		Provider: "jira", ExternalID: issueKey, Title: "Refund API",
	}); err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
}

func TestHandle_AutoLogOffIsANoOp(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: false, CommentEvents: []string{"delivery.started"}}
	hook, store, ob, fc := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	drainOutbox(t, ob, hook.registry, nil)
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none when auto_log is off", fc.calls)
	}
}

func TestHandle_NoLinkedIssueSkipsSilently(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	hook, store, ob, fc := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	// No CaptureRequirement call at all: this delivery has no Jira-sourced
	// requirement captured.

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	drainOutbox(t, ob, hook.registry, nil)
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none when no Jira issue is linked", fc.calls)
	}
}

func TestHandle_PostsCommentForConfiguredEventAndDedupesOnRetry(t *testing.T) {
	// EventDeliveryStarted is now handled entirely by jiraintegration.Service
	// (see jirahooks.go's Handle), which builds its own comment from the
	// orchestration's actually-persisted title/projects rather than trusting
	// whatever an event happens to carry - so this test attaches a real
	// title and project instead of only setting them on a synthetic event,
	// matching how internal/delivery's own CreateOrchestration dispatch
	// always populates Title/Projects from the just-persisted record.
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	hook, store, ob, fc := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestrationWithOptions(ctx, "create-1", delivery.NewID(), nil, delivery.OrchestrationOptions{Title: "Refund delivery"})
	if err != nil {
		t.Fatalf("CreateOrchestrationWithOptions: %v", err)
	}
	project, err := store.RegisterProject(ctx, "register-proj-a", delivery.NewID(), "proj-a", "https://example.test/proj-a.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	if _, err := store.AttachProject(ctx, "attach-proj-a", orch.Id, orch.Revision, project.Id); err != nil {
		t.Fatalf("AttachProject: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	event := deliveryhooks.Event{Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1}
	if err := hook.Handle(ctx, event); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	// A retried dispatch of the exact same (delivery, event type, revision)
	// must not enqueue a second intent.
	if err := hook.Handle(ctx, event); err != nil {
		t.Fatalf("Handle (retry): %v", err)
	}
	drainOutbox(t, ob, hook.registry, nil)

	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.addJiraComment" {
		t.Fatalf("calls = %+v, want exactly one addJiraComment call", fc.calls)
	}
	if fc.calls[0]["issueIdOrKey"] != "PAY-1" {
		t.Fatalf("issueIdOrKey = %v, want PAY-1", fc.calls[0]["issueIdOrKey"])
	}
	body, _ := fc.calls[0]["commentBody"].(string)
	for _, want := range []string{"delivery started", "Refund delivery", project.Id, orch.Id} {
		if !strings.Contains(body, want) {
			t.Errorf("commentBody = %q, want it to contain %q", body, want)
		}
	}
}

func TestHandle_LaneEventsDeduplicatePerLane(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"implementation.started", "implementation.completed"}}
	hook, store, ob, fc := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	for _, eventType := range []deliveryhooks.EventType{
		deliveryhooks.EventImplementationStarted,
		deliveryhooks.EventImplementationCompleted,
	} {
		for _, laneID := range []string{"lane-a", "lane-b"} {
			event := deliveryhooks.Event{
				Type: eventType, DeliveryID: orch.Id, Revision: 7,
				EntityID: laneID, Summary: string(eventType) + " on " + laneID,
			}
			if err := hook.Handle(ctx, event); err != nil {
				t.Fatalf("Handle(%s, %s): %v", eventType, laneID, err)
			}
			if err := hook.Handle(ctx, event); err != nil {
				t.Fatalf("Handle retry(%s, %s): %v", eventType, laneID, err)
			}
		}
	}
	drainOutbox(t, ob, hook.registry, nil)

	if len(fc.calls) != 4 {
		t.Fatalf("calls = %d, want one comment for each event type and lane; calls=%+v", len(fc.calls), fc.calls)
	}
}

func TestHandle_EventNotInCommentEventsDoesNothing(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.completed"}}
	hook, store, ob, fc := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	drainOutbox(t, ob, hook.registry, nil)
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none for an event type not in comment_events", fc.calls)
	}
}

func TestHandle_TransitionOnCompleteFiresMatchingTransition(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, TransitionOnComplete: true}
	hook, store, ob, fc := newTestHook(t, cfg)
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

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryCompleted, DeliveryID: orch.Id, Revision: 1}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	drainOutbox(t, ob, hook.registry, nil)

	var sawTransitions, sawTransition bool
	for _, c := range fc.calls {
		switch c["op"] {
		case "atlassian.getTransitionsForJiraIssue":
			sawTransitions = true
		case "atlassian.transitionJiraIssue":
			sawTransition = true
			if c["transitionId"] != "31" {
				t.Errorf("transitionId = %v, want 31", c["transitionId"])
			}
		}
	}
	if !sawTransitions || !sawTransition {
		t.Fatalf("calls = %+v, want getTransitionsForJiraIssue then transitionJiraIssue", fc.calls)
	}
}

func TestHandle_TransitionOnCompleteFalseNeverCallsTransition(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, TransitionOnComplete: false}
	hook, store, ob, fc := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryCompleted, DeliveryID: orch.Id, Revision: 1}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	drainOutbox(t, ob, hook.registry, nil)
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none with transition_on_complete false and no comment_events configured", fc.calls)
	}
}

// TestHandle_FailedWriteStaysRetryingWithoutDuplicatingTheIntent replaces
// the pre-outbox version of this test (which asserted Handle itself
// returned the adapter's error synchronously). Handle now only enqueues -
// see jirahooks.go's package doc comment - so a write failure surfaces as
// the outbox intent moving to retrying, not as an error from Handle; a
// later dispatch of the same event must not create a second, competing
// intent.
func TestHandle_FailedWriteStaysRetryingWithoutDuplicatingTheIntent(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	hook, store, ob, fc := newTestHook(t, cfg)
	fc.failOps = map[string]bool{"atlassian.addJiraComment": true}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")
	event := deliveryhooks.Event{Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1}

	if err := hook.Handle(ctx, event); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	drainOutbox(t, ob, hook.registry, nil)
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one attempted addJiraComment call", fc.calls)
	}
	intent, err := ob.GetByFingerprint(ctx, providerwrite.JiraCommentFingerprint(orch.Id, string(event.Type), "PAY-1"))
	if err != nil {
		t.Fatalf("GetByFingerprint: %v", err)
	}
	if intent.Status != outbox.StatusRetrying {
		t.Fatalf("status = %q, want retrying", intent.Status)
	}

	// A retried dispatch of the same event must resolve to the same
	// fingerprint rather than enqueue a competing second intent.
	if err := hook.Handle(ctx, event); err != nil {
		t.Fatalf("Handle (retry): %v", err)
	}
	stillOne, err := ob.GetByFingerprint(ctx, providerwrite.JiraCommentFingerprint(orch.Id, string(event.Type), "PAY-1"))
	if err != nil {
		t.Fatalf("GetByFingerprint: %v", err)
	}
	if stillOne.ID != intent.ID {
		t.Fatalf("expected the same intent id, got a different one: %q vs %q", stillOne.ID, intent.ID)
	}
}

func TestHandle_ProjectsExplicitWorklogToExactJiraTask(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, LogWork: true}
	hook, store, ob, fc := newTestHook(t, cfg)
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

	event := deliveryhooks.Event{
		Type: deliveryhooks.EventWorkLogged, DeliveryID: orch.Id, EntityID: entry.ID, Revision: 2,
		JiraIssueKey: entry.JiraIssueKey, DurationSeconds: entry.DurationSeconds, Summary: entry.Summary,
	}
	if err := hook.Handle(ctx, event); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	drainOutbox(t, ob, hook.registry, NewWorklogSyncObserver(store))

	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.addWorklog" {
		t.Fatalf("calls = %+v, want one addWorklog", fc.calls)
	}
	if fc.calls[0]["issueIdOrKey"] != "PAY-1901" || fc.calls[0]["timeSpentSeconds"] != 300 {
		t.Fatalf("worklog call = %+v, want exact task and duration", fc.calls[0])
	}
	intent, err := ob.GetByFingerprint(ctx, providerwrite.JiraWorklogFingerprint(entry.ID))
	if err != nil {
		t.Fatalf("GetByFingerprint: %v", err)
	}
	if intent.Status != outbox.StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", intent.Status)
	}
	synced, err := store.GetWorkLog(ctx, orch.Id, entry.ID)
	if err != nil {
		t.Fatalf("GetWorkLog: %v", err)
	}
	if synced.SyncStatus != "synced" {
		t.Fatalf("sync status = %q, want synced (observer must mark the ledger entry synced)", synced.SyncStatus)
	}
}
