package outbox

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

func newOutbox(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db)
}

func jiraCommentIntent(fingerprint string) Intent {
	return Intent{
		OrchestrationID:      "orch-1",
		AdapterID:            "atlassian",
		Operation:            "atlassian.addJiraComment",
		TargetKey:            "ABC-1",
		PayloadJSON:          `{"commentBody":"hi"}`,
		OperationFingerprint: fingerprint,
	}
}

func mustEnqueue(t *testing.T, store *Store, intent Intent) Intent {
	t.Helper()
	out, err := store.Enqueue(context.Background(), intent)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return out
}

// claimConcurrently fires n concurrent Claim calls at store and returns how
// many actually obtained the one enqueued intent.
func claimConcurrently(t *testing.T, store *Store, n int) int {
	t.Helper()
	var claimed int64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			intent, err := store.Claim(context.Background(), "worker-"+string(rune('a'+worker)), time.Now(), time.Minute)
			if err != nil {
				t.Errorf("Claim: %v", err)
				return
			}
			if intent != nil {
				atomic.AddInt64(&claimed, 1)
			}
		}(i)
	}
	wg.Wait()
	return int(claimed)
}

func TestOnlyOneWorkerClaimsAnIntent(t *testing.T) {
	store := newOutbox(t)
	mustEnqueue(t, store, jiraCommentIntent("intent-1"))
	claimed := claimConcurrently(t, store, 8)
	if claimed != 1 {
		t.Fatalf("claimed = %d, want exactly 1", claimed)
	}
}

func TestEnqueueIsIdempotentByFingerprint(t *testing.T) {
	store := newOutbox(t)
	ctx := context.Background()
	first := mustEnqueue(t, store, jiraCommentIntent("intent-1"))
	second := mustEnqueue(t, store, jiraCommentIntent("intent-1"))
	if first.ID != second.ID {
		t.Fatalf("enqueueing the same fingerprint twice produced different rows: %q vs %q", first.ID, second.ID)
	}

	worker, err := store.Claim(ctx, "worker-a", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if worker == nil {
		t.Fatal("expected the enqueued intent to be claimable")
	}
	if _, err := store.Succeed(ctx, worker.ID, "worker-a", "ext-1", "req-1", nil); err != nil {
		t.Fatalf("Succeed: %v", err)
	}

	// Re-enqueuing after success must return the same succeeded row, never
	// resurrect it back to pending.
	third := mustEnqueue(t, store, jiraCommentIntent("intent-1"))
	if third.Status != StatusSucceeded {
		t.Fatalf("status = %q, want succeeded to be preserved across a duplicate enqueue", third.Status)
	}
}

func TestClaimReclaimsAnExpiredLease(t *testing.T) {
	store := newOutbox(t)
	ctx := context.Background()
	mustEnqueue(t, store, jiraCommentIntent("intent-1"))

	past := time.Now().Add(-time.Hour)
	first, err := store.Claim(ctx, "worker-a", past, time.Millisecond)
	if err != nil || first == nil {
		t.Fatalf("Claim (first): intent=%v err=%v", first, err)
	}

	// worker-a's lease already expired (it claimed with a 1ms lease an hour
	// ago); a different worker must be able to reclaim it now.
	second, err := store.Claim(ctx, "worker-b", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("Claim (second): %v", err)
	}
	if second == nil || second.ID != first.ID {
		t.Fatalf("expected worker-b to reclaim the expired lease, got %+v", second)
	}
	if second.ClaimOwner != "worker-b" {
		t.Fatalf("claim_owner = %q, want worker-b", second.ClaimOwner)
	}
}

func TestSucceedFailsWhenNotOwnedByCallingWorker(t *testing.T) {
	store := newOutbox(t)
	ctx := context.Background()
	mustEnqueue(t, store, jiraCommentIntent("intent-1"))
	claimed, err := store.Claim(ctx, "worker-a", time.Now(), time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("Claim: intent=%v err=%v", claimed, err)
	}
	if _, err := store.Succeed(ctx, claimed.ID, "worker-b", "ext-1", "req-1", nil); err == nil {
		t.Fatal("expected Succeed to reject a worker that does not own the claim")
	}
}

func TestRetrySchedulesNextAttemptAndAllowsReclaim(t *testing.T) {
	store := newOutbox(t)
	ctx := context.Background()
	mustEnqueue(t, store, jiraCommentIntent("intent-1"))
	claimed, err := store.Claim(ctx, "worker-a", time.Now(), time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("Claim: intent=%v err=%v", claimed, err)
	}
	retryAt := time.Now().Add(-time.Second) // already due
	resolved, err := store.Retry(ctx, claimed.ID, "worker-a", "temporary", "adapter timed out", retryAt)
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if resolved.Status != StatusRetrying {
		t.Fatalf("status = %q, want retrying", resolved.Status)
	}

	reclaimed, err := store.Claim(ctx, "worker-b", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("Claim (reclaim): %v", err)
	}
	if reclaimed == nil || reclaimed.ID != claimed.ID {
		t.Fatalf("expected the due retry to be reclaimable, got %+v", reclaimed)
	}
}

func TestMarkAmbiguousMovesToReconcilingAndRecordsThePriorOutcome(t *testing.T) {
	store := newOutbox(t)
	ctx := context.Background()
	mustEnqueue(t, store, jiraCommentIntent("intent-1"))
	claimed, err := store.Claim(ctx, "worker-a", time.Now(), time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("Claim: intent=%v err=%v", claimed, err)
	}
	resolved, err := store.MarkAmbiguous(ctx, claimed.ID, "worker-a", "req-1", "timed out waiting for a response")
	if err != nil {
		t.Fatalf("MarkAmbiguous: %v", err)
	}
	if resolved.Status != StatusReconciling {
		t.Fatalf("status = %q, want reconciling", resolved.Status)
	}

	// A reconciling row is claimable again (nothing else currently owns
	// it) - what stops a blind replay is that the claimer sees the prior
	// outcome was ambiguous and must reconcile before ever writing again;
	// that policy lives in internal/providerwrite, not here.
	reclaimed, err := store.Claim(ctx, "worker-b", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if reclaimed == nil || reclaimed.ID != claimed.ID {
		t.Fatalf("expected the reconciling intent to be reclaimable, got %+v", reclaimed)
	}
	outcome, found, err := store.LastAttemptOutcome(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("LastAttemptOutcome: %v", err)
	}
	if !found || outcome != "ambiguous" {
		t.Fatalf("LastAttemptOutcome = (%q, %v), want (ambiguous, true)", outcome, found)
	}
}

func TestCancelNeverChangesASucceededIntentAndBlocksFurtherClaims(t *testing.T) {
	store := newOutbox(t)
	ctx := context.Background()
	mustEnqueue(t, store, jiraCommentIntent("intent-1"))
	claimed, err := store.Claim(ctx, "worker-a", time.Now(), time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("Claim: intent=%v err=%v", claimed, err)
	}
	if _, err := store.Succeed(ctx, claimed.ID, "worker-a", "ext-1", "req-1", nil); err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	after, err := store.Cancel(ctx, claimed.ID, "no longer needed")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if after.Status != StatusSucceeded {
		t.Fatalf("status = %q, cancellation must never change a succeeded intent", after.Status)
	}

	mustEnqueue(t, store, jiraCommentIntent("intent-2"))
	second, err := store.Claim(ctx, "worker-a", time.Now(), time.Minute)
	if err != nil || second == nil {
		t.Fatalf("Claim: intent=%v err=%v", second, err)
	}
	cancelled, err := store.Cancel(ctx, second.ID, "superseded")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if cancelled.Status != StatusCancelled {
		t.Fatalf("status = %q, want cancelled", cancelled.Status)
	}
	blocked, err := store.Claim(ctx, "worker-b", time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if blocked != nil {
		t.Fatalf("expected a cancelled intent to never be claimable again, got %+v", blocked)
	}
}

func TestSucceedRecordsGranularEffects(t *testing.T) {
	store := newOutbox(t)
	ctx := context.Background()
	mustEnqueue(t, store, jiraCommentIntent("intent-1"))
	claimed, err := store.Claim(ctx, "worker-a", time.Now(), time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("Claim: intent=%v err=%v", claimed, err)
	}
	effects := []Effect{
		{IntentID: claimed.ID, EffectKey: "ABC-2"},
		{IntentID: claimed.ID, EffectKey: "ABC-3"},
	}
	if _, err := store.Succeed(ctx, claimed.ID, "worker-a", "", "", effects); err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	got, err := store.ListEffects(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("ListEffects: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListEffects = %+v, want 2 rows", got)
	}
}
