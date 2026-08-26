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
	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/deliveryhooks"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
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

func approvalRequired() *protocol.AdapterManifestOperationsValueApproval {
	v := protocol.AdapterManifestOperationsValueApprovalRequired
	return &v
}

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
			"atlassian.getTransitionsForJiraIssue": {SideEffect: false},
			"atlassian.addJiraComment":             {SideEffect: true, Approval: approvalRequired()},
			"atlassian.addWorklog":                 {SideEffect: true, Approval: approvalRequired()},
			"atlassian.transitionJiraIssue":        {SideEffect: true, Approval: approvalRequired()},
		},
	}
}

// fakeGateResolver hands back a pre-built Gate instead of spawning a real
// adapter subprocess, satisfying JiraHook's gateResolver dependency.
type fakeGateResolver struct{ gate *adapters.Gate }

func (f *fakeGateResolver) Gate(ctx context.Context, adapterID string) (*adapters.Gate, error) {
	return f.gate, nil
}

// newTestHook builds a JiraHook wired to a fake adapter caller and a fresh
// delivery.Store/storage kernel. Returns the hook, the underlying
// delivery.Store (for capturing requirements), the fake caller (for
// asserting on calls made), and an approve func a test calls with a
// delivery id - Handle's own runID is the delivery id (see JiraHook.Handle),
// and internal/adapters.Gate scopes one approval per run id, so approving
// for a run must happen after that delivery id is known, not up front.
func newTestHook(t *testing.T, cfg *jiraworkflow.Config) (hook *JiraHook, store *delivery.Store, fc *fakeAdapterCaller, approve func(deliveryID string)) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	approvalStore := approvals.New(db, "test-project")
	fc = &fakeAdapterCaller{}
	gate := adapters.NewGate("atlassian", testManifest(), fc, approvalStore)

	store = delivery.NewStore(db)
	hook = &JiraHook{db: db, store: store, registry: &fakeGateResolver{gate: gate}, cfg: cfg}
	approve = func(deliveryID string) {
		t.Helper()
		// addJiraComment and transitionJiraIssue share one run-scoped
		// approval (internal/adapters.Gate's own semantics), so requesting
		// against either op and approving covers both.
		if _, err := gate.RequestApproval(deliveryID, "atlassian.addJiraComment", protocol.ApprovalRecordRequestedBySemar); err != nil {
			t.Fatalf("RequestApproval(addJiraComment): %v", err)
		}
		if err := gate.Approve(deliveryID, "tester"); err != nil {
			t.Fatalf("Approve: %v", err)
		}
	}
	return hook, store, fc, approve
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
	hook, store, fc, _ := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none when auto_log is off", fc.calls)
	}
}

func TestHandle_NoLinkedIssueSkipsSilently(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	hook, store, fc, _ := newTestHook(t, cfg)
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
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none when no Jira issue is linked", fc.calls)
	}
}

func TestHandle_PostsCommentForConfiguredEventAndDedupesOnRetry(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	hook, store, fc, approve := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")
	approve(orch.Id)

	event := deliveryhooks.Event{
		Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1,
		Title: "Refund delivery", Projects: []string{"proj-a"},
	}
	if err := hook.Handle(ctx, event); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.addJiraComment" {
		t.Fatalf("calls = %+v, want exactly one addJiraComment call", fc.calls)
	}
	if fc.calls[0]["issueIdOrKey"] != "PAY-1" {
		t.Fatalf("issueIdOrKey = %v, want PAY-1", fc.calls[0]["issueIdOrKey"])
	}
	body, _ := fc.calls[0]["commentBody"].(string)
	for _, want := range []string{"delivery started", "Refund delivery", "proj-a", orch.Id} {
		if !strings.Contains(body, want) {
			t.Errorf("commentBody = %q, want it to contain %q", body, want)
		}
	}

	// A retried dispatch of the exact same (delivery, event type, revision)
	// must not post a second comment.
	if err := hook.Handle(ctx, event); err != nil {
		t.Fatalf("Handle (retry): %v", err)
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls after retry = %+v, want still exactly one (deduped)", fc.calls)
	}
}

func TestHandle_LaneEventsDeduplicatePerLane(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"implementation.started", "implementation.completed"}}
	hook, store, fc, approve := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")
	approve(orch.Id)

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

	if len(fc.calls) != 4 {
		t.Fatalf("calls = %d, want one comment for each event type and lane; calls=%+v", len(fc.calls), fc.calls)
	}
}

func TestHandle_EventNotInCommentEventsDoesNothing(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.completed"}}
	hook, store, fc, _ := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none for an event type not in comment_events", fc.calls)
	}
}

func TestHandle_TransitionOnCompleteFiresMatchingTransition(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, TransitionOnComplete: true}
	hook, store, fc, approve := newTestHook(t, cfg)
	fc.responses = map[string]string{
		"atlassian.getTransitionsForJiraIssue": `{"transitions":[{"id":"31","name":"Close","toStatus":{"id":"3","name":"Done"}}]}`,
	}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")
	approve(orch.Id)

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryCompleted, DeliveryID: orch.Id, Revision: 1}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

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
	hook, store, fc, _ := newTestHook(t, cfg)
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryCompleted, DeliveryID: orch.Id, Revision: 1}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none with transition_on_complete false and no comment_events configured", fc.calls)
	}
}

func TestHandle_FailedCommentIsNotMarkedFiredAndRetriesLater(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	hook, store, fc, approve := newTestHook(t, cfg)
	fc.failOps = map[string]bool{"atlassian.addJiraComment": true}
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")
	approve(orch.Id)
	event := deliveryhooks.Event{Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1}

	if err := hook.Handle(ctx, event); err == nil {
		t.Fatal("expected the simulated adapter failure to surface")
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one attempted addJiraComment call", fc.calls)
	}

	// Not marked fired: a later dispatch of the same event must retry, not
	// silently skip.
	fc.failOps = nil
	if err := hook.Handle(ctx, event); err != nil {
		t.Fatalf("Handle (retry after transient failure cleared): %v", err)
	}
	if len(fc.calls) != 2 {
		t.Fatalf("calls = %+v, want a second attempted addJiraComment call", fc.calls)
	}
}

func TestHandle_ProjectsExplicitWorklogToExactJiraTask(t *testing.T) {
	cfg := &jiraworkflow.Config{AutoLog: true, LogWork: true}
	hook, store, fc, approve := newTestHook(t, cfg)
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
	approve(orch.Id)

	event := deliveryhooks.Event{
		Type: deliveryhooks.EventWorkLogged, DeliveryID: orch.Id, EntityID: entry.ID, Revision: 2,
		JiraIssueKey: entry.JiraIssueKey, DurationSeconds: entry.DurationSeconds, Summary: entry.Summary,
	}
	if err := hook.Handle(ctx, event); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fc.calls) != 1 || fc.calls[0]["op"] != "atlassian.addWorklog" {
		t.Fatalf("calls = %+v, want one addWorklog", fc.calls)
	}
	if fc.calls[0]["issueIdOrKey"] != "PAY-1901" || fc.calls[0]["timeSpentSeconds"] != 300 {
		t.Fatalf("worklog call = %+v, want exact task and duration", fc.calls[0])
	}
	synced, err := store.GetWorkLog(ctx, orch.Id, entry.ID)
	if err != nil {
		t.Fatalf("GetWorkLog: %v", err)
	}
	if synced.SyncStatus != "synced" {
		t.Fatalf("sync status = %q, want synced", synced.SyncStatus)
	}
}

func TestHandle_MissingApprovalFailsAndIsSwallowableByCaller(t *testing.T) {
	// A hook fired outside any MCP tool call has no session to elicit an
	// approval decision from - gate.Call fails closed exactly like any
	// other unapproved adapter write, and Handle surfaces that as an
	// ordinary error for its caller (deliveryhooks.Dispatcher) to log and
	// swallow.
	cfg := &jiraworkflow.Config{AutoLog: true, CommentEvents: []string{"delivery.started"}}
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	approvalStore := approvals.New(db, "test-project")
	fc := &fakeAdapterCaller{}
	gate := adapters.NewGate("atlassian", testManifest(), fc, approvalStore) // never approved
	store := delivery.NewStore(db)
	hook := &JiraHook{db: db, store: store, registry: &fakeGateResolver{gate: gate}, cfg: cfg}

	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	captureJiraRequirement(t, store, orch.Id, "PAY-1")

	if err := hook.Handle(ctx, deliveryhooks.Event{Type: deliveryhooks.EventDeliveryStarted, DeliveryID: orch.Id, Revision: 1}); err == nil {
		t.Fatal("expected an error when addJiraComment has not been approved")
	}
}
