package providerwrite

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func newOutboxStore(t *testing.T) *outbox.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return outbox.New(db)
}

// permissiveInputSchema is just enough of an input_schema to satisfy Gate's
// per-call payload validation without constraining which params a test may
// pass - these tests exercise outbox/worker/reconciler behavior, not
// adapter schema strictness.
var permissiveInputSchema = protocol.AdapterManifestOperationsValueInputSchema{"type": "object"}

func atlassianManifest() protocol.AdapterManifest {
	return protocol.AdapterManifest{
		Id: "atlassian", Name: "atlassian", Version: "0.1.0", Protocol: "punakawan.adapter/v1",
		Runtime: protocol.AdapterManifestRuntimeNode, Provides: []string{"jira"},
		Permissions: protocol.AdapterManifestPermissions{
			Network:    protocol.AdapterManifestPermissionsNetwork{Hosts: []string{"api.atlassian.com"}},
			Filesystem: protocol.AdapterManifestPermissionsFilesystem{Read: []string{}, Write: []string{}},
			Secrets:    []string{},
		},
		Operations: protocol.AdapterManifestOperations{
			"atlassian.getJiraIssue":               {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.getJiraComments":            {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.addJiraComment":             {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.getTransitionsForJiraIssue": {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.transitionJiraIssue":        {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.addWorklog":                 {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.listJiraWorklogs":           {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.createJiraSubtask":          {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"atlassian.editJiraIssue":              {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
		},
	}
}

type fakeComment struct {
	id   string
	body string
}

// fakeProvider is a minimal fake adapter transport (satisfying the same
// Call(ctx, method, params) method set internal/adapters.Client exposes to
// Gate) used to drive Worker end to end without a real subprocess.
type fakeProvider struct {
	mu                sync.Mutex
	comments          []fakeComment
	nextCommentID     int
	writeAttempts     int
	loseFirstResponse bool
	lostOnce          bool
}

func (f *fakeProvider) WriteAttempts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeAttempts
}

func (f *fakeProvider) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args, _ := params.(map[string]any)
	op, _ := args["op"].(string)
	f.mu.Lock()
	defer f.mu.Unlock()

	switch op {
	case "atlassian.addJiraComment":
		f.writeAttempts++
		f.nextCommentID++
		id := fmt.Sprintf("c-%d", f.nextCommentID)
		body, _ := args["commentBody"].(string)
		f.comments = append(f.comments, fakeComment{id: id, body: body})
		if f.loseFirstResponse && !f.lostOnce {
			f.lostOnce = true
			return nil, fmt.Errorf("connection reset while reading response: %w", ErrResponseLost)
		}
		raw, _ := json.Marshal(map[string]any{"commentId": id})
		return raw, nil
	case "atlassian.getJiraComments":
		out := make([]map[string]any, 0, len(f.comments))
		for _, c := range f.comments {
			out = append(out, map[string]any{"id": c.id, "body": c.body})
		}
		raw, _ := json.Marshal(map[string]any{"comments": out})
		return raw, nil
	default:
		return nil, fmt.Errorf("fakeProvider: unhandled op %q", op)
	}
}

type fakeResolver struct {
	gate *adapters.Gate
}

func (r fakeResolver) Gate(ctx context.Context, adapterID string) (*adapters.Gate, error) {
	return r.gate, nil
}

func newWorkerHarness(t *testing.T, remote *fakeProvider) (*outbox.Store, *Worker) {
	t.Helper()
	store := newOutboxStore(t)
	gate := adapters.NewGate("atlassian", atlassianManifest(), remote)
	worker := &Worker{ID: "worker-1", Store: store, Adapters: fakeResolver{gate: gate}}
	return store, worker
}

func jiraCommentIntent(fingerprint string) outbox.Intent {
	payload, _ := json.Marshal(map[string]any{"comment_body": "hello"})
	return outbox.Intent{
		OrchestrationID: "orch-1", AdapterID: "atlassian", Operation: "atlassian.addJiraComment",
		TargetKey: "ABC-1", PayloadJSON: string(payload), OperationFingerprint: fingerprint,
	}
}

// TestSuccessfulRemoteWriteWithLostLocalAckReconcilesWithoutReplay is the
// exact regression this reconciliation contract exists for: a write that
// applied remotely but whose response was lost locally must never be
// posted a second time, and the second run resolves it purely by
// re-reading remote state.
func TestSuccessfulRemoteWriteWithLostLocalAckReconcilesWithoutReplay(t *testing.T) {
	remote := &fakeProvider{loseFirstResponse: true}
	store, worker := newWorkerHarness(t, remote)
	ctx := context.Background()

	if _, err := store.Enqueue(ctx, jiraCommentIntent("intent-1")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	did, err := worker.RunOnce(ctx)
	if err != nil || !did {
		t.Fatalf("RunOnce (first): did=%v err=%v", did, err)
	}
	if remote.WriteAttempts() != 1 {
		t.Fatalf("WriteAttempts after first run = %d, want 1", remote.WriteAttempts())
	}
	intent, err := store.GetByFingerprint(ctx, "intent-1")
	if err != nil {
		t.Fatalf("GetByFingerprint: %v", err)
	}
	if intent.Status != outbox.StatusReconciling {
		t.Fatalf("status after ambiguous attempt = %q, want reconciling", intent.Status)
	}

	did, err = worker.RunOnce(ctx)
	if err != nil || !did {
		t.Fatalf("RunOnce (second): did=%v err=%v", did, err)
	}
	if remote.WriteAttempts() != 1 {
		t.Fatalf("WriteAttempts after second run = %d, want still 1 (no replay)", remote.WriteAttempts())
	}
	intent, err = store.GetByFingerprint(ctx, "intent-1")
	if err != nil {
		t.Fatalf("GetByFingerprint: %v", err)
	}
	if intent.Status != outbox.StatusSucceeded {
		t.Fatalf("status after reconciliation = %q, want succeeded", intent.Status)
	}
	if intent.ExternalID == "" {
		t.Fatal("expected reconciliation to record the comment's external id")
	}
}

func TestExecuteRetriesOnAnOrdinaryAdapterError(t *testing.T) {
	remote := &fakeProvider{}
	store, worker := newWorkerHarness(t, remote)
	ctx := context.Background()

	// No comments configured means atlassian.addJiraComment still succeeds
	// (the fake always applies), so instead exercise the retry path via an
	// operation with no registered executor, which must retry with a clear
	// diagnostic rather than error out of RunOnce.
	intent := outbox.Intent{
		OrchestrationID: "orch-1", AdapterID: "atlassian", Operation: "atlassian.doesNotExist",
		TargetKey: "ABC-1", PayloadJSON: `{}`, OperationFingerprint: "intent-2",
	}
	if _, err := store.Enqueue(ctx, intent); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	got, err := store.GetByFingerprint(ctx, "intent-2")
	if err != nil {
		t.Fatalf("GetByFingerprint: %v", err)
	}
	if got.Status != outbox.StatusRetrying {
		t.Fatalf("status = %q, want retrying", got.Status)
	}
	if got.LastErrorCode != "no_executor" {
		t.Fatalf("last_error_code = %q, want no_executor", got.LastErrorCode)
	}
}

func TestRunOnceIsANoOpWhenNothingIsClaimable(t *testing.T) {
	remote := &fakeProvider{}
	_, worker := newWorkerHarness(t, remote)
	did, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if did {
		t.Fatal("expected RunOnce to report no work on an empty outbox")
	}
}

func TestPoolStartStopClaimsAndDrainsCleanly(t *testing.T) {
	remote := &fakeProvider{}
	store, _ := newWorkerHarness(t, remote)
	ctx := context.Background()
	if _, err := store.Enqueue(ctx, jiraCommentIntent("intent-1")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	pool := NewPool(store, fakeResolver{gate: adapters.NewGate("atlassian", atlassianManifest(), remote)}, 2, 10*time.Millisecond, nil)
	pool.Start(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for {
		intent, err := store.GetByFingerprint(ctx, "intent-1")
		if err != nil {
			t.Fatalf("GetByFingerprint: %v", err)
		}
		if intent.Status == outbox.StatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("intent never succeeded via the pool, last status %q", intent.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
