package telemetry

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

func newTelemetryStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

func mustBegin(t *testing.T, store *Store, req BeginRequest) AgentSession {
	t.Helper()
	out, err := store.Begin(context.Background(), req)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return out
}

func mustSnapshot(t *testing.T, store *Store, req SnapshotRequest) UsageProjection {
	t.Helper()
	out, err := store.IngestSnapshot(context.Background(), req)
	if err != nil {
		t.Fatalf("IngestSnapshot: %v", err)
	}
	return out
}

func mustTotals(t *testing.T, store *Store, orchestrationID string) UsageProjection {
	t.Helper()
	out, err := store.TotalsByDelivery(context.Background(), orchestrationID)
	if err != nil {
		t.Fatalf("TotalsByDelivery: %v", err)
	}
	return out
}

type finalizeOutcome struct {
	Session    AgentSession
	Projection UsageProjection
}

// sameFinalizeOutcome compares two finalizeOutcomes by value: AgentSession
// and UsageProjection both carry pointer fields (StoppedAt, EstimatedCost)
// that Go's == would compare by address, not by pointee value, which would
// make two outcomes fetched from separate GetSession/TotalsByDelivery
// calls spuriously unequal even when every observable field matches.
func sameFinalizeOutcome(a, b finalizeOutcome) bool {
	sa, sb := a.Session, b.Session
	sa.StoppedAt, sb.StoppedAt = nil, nil
	if sa != sb {
		return false
	}
	if (a.Session.StoppedAt == nil) != (b.Session.StoppedAt == nil) {
		return false
	}
	if a.Session.StoppedAt != nil && !a.Session.StoppedAt.Equal(*b.Session.StoppedAt) {
		return false
	}
	pa, pb := a.Projection, b.Projection
	if !slices.Equal(pa.UnpricedModels, pb.UnpricedModels) {
		return false
	}
	if pa.OrchestrationID != pb.OrchestrationID || pa.Counters != pb.Counters ||
		pa.TotalTokens != pb.TotalTokens || pa.TelemetryStatus != pb.TelemetryStatus {
		return false
	}
	if (a.Projection.EstimatedCost == nil) != (b.Projection.EstimatedCost == nil) {
		return false
	}
	if a.Projection.EstimatedCost != nil && *a.Projection.EstimatedCost != *b.Projection.EstimatedCost {
		return false
	}
	return true
}

func mustFinalize(t *testing.T, store *Store, req FinalizeRequest) finalizeOutcome {
	t.Helper()
	session, projection, err := store.Finalize(context.Background(), req)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	return finalizeOutcome{Session: session, Projection: projection}
}

func TestSnapshotReplayAndOlderSequenceAreNoOps(t *testing.T) {
	store := newTelemetryStore(t)
	s := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})

	mustSnapshot(t, store, SnapshotRequest{SessionID: s.ID, SourceID: "main", Sequence: 2, InputTokens: 100, OutputTokens: 40, ToolCalls: 3, ElapsedMS: 9000})
	mustSnapshot(t, store, SnapshotRequest{SessionID: s.ID, SourceID: "main", Sequence: 2, InputTokens: 100, OutputTokens: 40, ToolCalls: 3, ElapsedMS: 9000})
	mustSnapshot(t, store, SnapshotRequest{SessionID: s.ID, SourceID: "main", Sequence: 1, InputTokens: 50, OutputTokens: 20, ToolCalls: 1, ElapsedMS: 4000})

	want := UsageTotals{InputTokens: 100, OutputTokens: 40, ToolCalls: 3, ElapsedMS: 9000}
	if got := mustTotals(t, store, "d1").Counters; got != want {
		t.Fatalf("counters = %+v, want %+v", got, want)
	}
}

func TestFinalizeAtomicallyAppliesFinalSnapshotOnce(t *testing.T) {
	store := newTelemetryStore(t)
	s := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "claude-code", ExternalSessionID: "sess-1"})
	req := FinalizeRequest{
		SessionID: s.ID, StopID: "stop-1", StoppedAt: time.Unix(10, 0),
		Snapshot: &SnapshotRequest{SourceID: "main", Sequence: 5, InputTokens: 200, OutputTokens: 50, ToolCalls: 7, ElapsedMS: 10000},
	}

	first := mustFinalize(t, store, req)
	second := mustFinalize(t, store, req)
	if !sameFinalizeOutcome(first, second) {
		t.Fatalf("first finalize = %+v, second finalize = %+v, want identical", first, second)
	}
	if got := mustTotals(t, store, "d1").TotalTokens; got != 250 {
		t.Fatalf("total tokens = %d, want 250", got)
	}
	if first.Session.Status != "closed" {
		t.Fatalf("session status = %q, want closed", first.Session.Status)
	}
}

func TestTwoSessionsUnderSameLifetimeAreAdditive(t *testing.T) {
	store := newTelemetryStore(t)
	a := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ExecutionID: "e1", ClientKind: "claude-code", ExternalSessionID: "sess-a"})
	b := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ExecutionID: "e2", ClientKind: "claude-code", ExternalSessionID: "sess-b"})

	mustSnapshot(t, store, SnapshotRequest{SessionID: a.ID, SourceID: "main", Sequence: 1, InputTokens: 10, OutputTokens: 5})
	mustSnapshot(t, store, SnapshotRequest{SessionID: b.ID, SourceID: "main", Sequence: 1, InputTokens: 30, OutputTokens: 15})

	got := mustTotals(t, store, "d1")
	if got.Counters.InputTokens != 40 || got.Counters.OutputTokens != 20 {
		t.Fatalf("counters = %+v, want input=40 output=20", got.Counters)
	}
}

func TestTwoAdhocDeliveriesNeverShareTotals(t *testing.T) {
	store := newTelemetryStore(t)
	a := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-a"})
	b := mustBegin(t, store, BeginRequest{DeliveryID: "d2", ClientKind: "codex", ExternalSessionID: "thr-b"})

	mustSnapshot(t, store, SnapshotRequest{SessionID: a.ID, SourceID: "main", Sequence: 1, InputTokens: 111})
	mustSnapshot(t, store, SnapshotRequest{SessionID: b.ID, SourceID: "main", Sequence: 1, InputTokens: 222})

	totalsA := mustTotals(t, store, "d1")
	totalsB := mustTotals(t, store, "d2")
	if totalsA.Counters.InputTokens != 111 {
		t.Fatalf("delivery d1 input tokens = %d, want 111 (not sharing d2's)", totalsA.Counters.InputTokens)
	}
	if totalsB.Counters.InputTokens != 222 {
		t.Fatalf("delivery d2 input tokens = %d, want 222 (not sharing d1's)", totalsB.Counters.InputTokens)
	}
}

func TestBeginResumesExistingExternalSession(t *testing.T) {
	store := newTelemetryStore(t)
	first := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1", Participant: "semar"})
	second := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1", Participant: "different-should-be-ignored"})
	if first.ID != second.ID {
		t.Fatalf("resuming the same (client_kind, external_session_id) minted a new session: first=%s second=%s", first.ID, second.ID)
	}
	if second.Participant != "semar" {
		t.Fatalf("resumed session participant = %q, want the original %q preserved", second.Participant, "semar")
	}
}

func TestIngestSnapshotWithUnknownModelKeepsCostUnknownNeverZero(t *testing.T) {
	store := newTelemetryStore(t)
	s := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})

	projection := mustSnapshot(t, store, SnapshotRequest{
		SessionID: s.ID, SourceID: "main", Sequence: 1,
		InputTokens: 100, OutputTokens: 50,
		ModelUsage: []ModelUsage{{Model: "some-future-model-nobody-prices-yet", InputTokens: 100, OutputTokens: 50}},
	})
	if projection.EstimatedCost != nil {
		t.Fatalf("estimated cost = %+v, want nil (unknown, never fabricated as zero)", projection.EstimatedCost)
	}
	if projection.TelemetryStatus != "incomplete" {
		t.Fatalf("telemetry status = %q, want incomplete", projection.TelemetryStatus)
	}
	if projection.Counters.InputTokens != 100 || projection.Counters.OutputTokens != 50 {
		t.Fatalf("counters = %+v, want tokens preserved despite unknown price", projection.Counters)
	}

	session, err := store.GetSession(context.Background(), s.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.TelemetryStatus != "incomplete" {
		t.Fatalf("session telemetry status = %q, want incomplete", session.TelemetryStatus)
	}
}

func TestIngestSnapshotWithKnownModelComputesRealCost(t *testing.T) {
	store := newTelemetryStore(t)
	catalog := NewCatalog([]ModelRate{{
		Provider: "test", Model: "test-model", EffectiveAt: time.Unix(0, 0),
		InputPerMillion: 1_000_000, OutputPerMillion: 2_000_000, Currency: "USD",
	}})
	priced := NewStore(store.db, WithCatalog(catalog))
	s := mustBegin(t, priced, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})

	projection := mustSnapshot(t, priced, SnapshotRequest{
		SessionID: s.ID, SourceID: "main", Sequence: 1,
		ModelUsage: []ModelUsage{{Model: "test-model", InputTokens: 2, OutputTokens: 3}},
	})
	if projection.EstimatedCost == nil {
		t.Fatal("estimated cost = nil, want a known amount")
	}
	if !projection.EstimatedCost.FullyKnown {
		t.Fatalf("estimated cost = %+v, want fully known", projection.EstimatedCost)
	}
	want := 2.0 + 6.0 // 2 input * $1/token + 3 output * $2/token, at 1e6 rate/tokens scale cancelled by 1e6 tokens
	if got := projection.EstimatedCost.Amount; got != want {
		t.Fatalf("estimated cost amount = %v, want %v", got, want)
	}
}

// Claude Code reports locally generated messages under the pseudo-model
// "<synthetic>". Treating it as an unpriceable model dragged the whole
// snapshot - and every delivery containing one - to unknown cost.
func TestIngestSnapshotIgnoresNonBillablePseudoModels(t *testing.T) {
	store := newTelemetryStore(t)
	catalog := NewCatalog([]ModelRate{{
		Provider: "test", Model: "test-model", EffectiveAt: time.Unix(0, 0),
		InputPerMillion: 1_000_000, OutputPerMillion: 2_000_000, Currency: "USD",
	}})
	priced := NewStore(store.db, WithCatalog(catalog))
	s := mustBegin(t, priced, BeginRequest{DeliveryID: "d1", ClientKind: "claude-code", ExternalSessionID: "thr-1"})

	projection := mustSnapshot(t, priced, SnapshotRequest{
		SessionID: s.ID, SourceID: "main", Sequence: 1,
		ModelUsage: []ModelUsage{
			{Model: "test-model", InputTokens: 2, OutputTokens: 3},
			{Model: "<synthetic>", InputTokens: 900, OutputTokens: 900},
		},
	})
	if projection.EstimatedCost == nil {
		t.Fatal("estimated cost = nil, want the billable model still priced")
	}
	if !projection.EstimatedCost.FullyKnown {
		t.Fatalf("estimated cost = %+v, want fully known", projection.EstimatedCost)
	}
	if got, want := projection.EstimatedCost.Amount, 8.0; got != want {
		t.Fatalf("estimated cost amount = %v, want %v (the pseudo-model contributes nothing)", got, want)
	}

	// And the pseudo-model must not stand between the session and a
	// complete telemetry status once it closes.
	session, _, err := priced.Finalize(context.Background(), FinalizeRequest{SessionID: s.ID, StopID: "stop-1"})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if session.TelemetryStatus != "complete" {
		t.Fatalf("session telemetry status = %q, want complete", session.TelemetryStatus)
	}
}

// A snapshot whose only usage is non-billable costs nothing and names no
// currency; it must not take the currency slot from the priced snapshots
// beside it and turn the delivery into a currency mismatch.
func TestNonBillableOnlySnapshotDoesNotClaimTheDeliveryCurrency(t *testing.T) {
	store := newTelemetryStore(t)
	catalog := NewCatalog([]ModelRate{{
		Provider: "test", Model: "test-model", EffectiveAt: time.Unix(0, 0),
		InputPerMillion: 1_000_000, OutputPerMillion: 2_000_000, Currency: "USD",
	}})
	priced := NewStore(store.db, WithCatalog(catalog))
	s := mustBegin(t, priced, BeginRequest{DeliveryID: "d1", ClientKind: "claude-code", ExternalSessionID: "thr-1"})

	mustSnapshot(t, priced, SnapshotRequest{
		SessionID: s.ID, SourceID: "sub-1", Sequence: 1,
		ModelUsage: []ModelUsage{{Model: "<synthetic>", InputTokens: 10, OutputTokens: 10}},
	})
	projection := mustSnapshot(t, priced, SnapshotRequest{
		SessionID: s.ID, SourceID: "main", Sequence: 1,
		ModelUsage: []ModelUsage{{Model: "test-model", InputTokens: 2, OutputTokens: 3}},
	})
	if projection.EstimatedCost == nil || !projection.EstimatedCost.FullyKnown {
		t.Fatalf("estimated cost = %+v, want fully known", projection.EstimatedCost)
	}
	if got := projection.EstimatedCost.Currency; got != "USD" {
		t.Fatalf("currency = %q, want USD", got)
	}
}

// A finalize carrying no snapshot is how the delivery-completion sweep
// closes a session nobody's own lifecycle hook closed. It used to preserve
// whatever telemetry_status the session already had, and since
// IngestSnapshot only ever writes "incomplete", that meant a fully priced
// session closed by the sweep still reported its cost as not fully known.
func TestFinalizeWithoutASnapshotStillRecomputesTelemetryStatus(t *testing.T) {
	store := newTelemetryStore(t)
	catalog := NewCatalog([]ModelRate{{
		Provider: "test", Model: "test-model", EffectiveAt: time.Unix(0, 0),
		InputPerMillion: 1_000_000, OutputPerMillion: 2_000_000, Currency: "USD",
	}})
	priced := NewStore(store.db, WithCatalog(catalog))
	s := mustBegin(t, priced, BeginRequest{DeliveryID: "d1", ClientKind: "claude-code", ExternalSessionID: "thr-1"})

	mustSnapshot(t, priced, SnapshotRequest{
		SessionID: s.ID, SourceID: "main", Sequence: 1,
		ModelUsage: []ModelUsage{{Model: "test-model", InputTokens: 2, OutputTokens: 3}},
	})

	session, projection, err := priced.Finalize(context.Background(), FinalizeRequest{SessionID: s.ID, StopID: "stop-1", StopReason: "delivery_completed"})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if session.TelemetryStatus != "complete" {
		t.Fatalf("session telemetry status = %q, want complete", session.TelemetryStatus)
	}
	if projection.TelemetryStatus != "complete" {
		t.Fatalf("delivery telemetry status = %q, want complete", projection.TelemetryStatus)
	}
	if projection.EstimatedCost == nil || !projection.EstimatedCost.FullyKnown {
		t.Fatalf("estimated cost = %+v, want fully known", projection.EstimatedCost)
	}
}

// The other direction still has to hold: one unpriceable snapshot keeps
// the session incomplete no matter how it is closed.
func TestFinalizeKeepsIncompleteWhenAnySnapshotWasUnpriced(t *testing.T) {
	store := newTelemetryStore(t)
	s := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})
	mustSnapshot(t, store, SnapshotRequest{
		SessionID: s.ID, SourceID: "main", Sequence: 1,
		ModelUsage: []ModelUsage{{Model: "some-future-model-nobody-prices-yet", InputTokens: 1}},
	})

	session, _, err := store.Finalize(context.Background(), FinalizeRequest{SessionID: s.ID, StopID: "stop-1"})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if session.TelemetryStatus != "incomplete" {
		t.Fatalf("session telemetry status = %q, want incomplete", session.TelemetryStatus)
	}
}

// "Cost unknown" alone gives a reader nothing to act on. The delivery
// projection has to name the model whose price is missing.
func TestTotalsByDeliveryNamesTheModelsItCouldNotPrice(t *testing.T) {
	store := newTelemetryStore(t)
	s := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "claude-code", ExternalSessionID: "thr-1"})

	mustSnapshot(t, store, SnapshotRequest{
		SessionID: s.ID, SourceID: "main", Sequence: 1,
		ModelUsage: []ModelUsage{
			{Model: "some-future-model", InputTokens: 1},
			{Model: "<synthetic>", InputTokens: 1},
		},
	})
	projection := mustSnapshot(t, store, SnapshotRequest{
		SessionID: s.ID, SourceID: "sub-1", Sequence: 1,
		ModelUsage: []ModelUsage{
			{Model: "another-future-model", InputTokens: 1},
			// Repeated across sources - it must be named once, not twice.
			{Model: "some-future-model", InputTokens: 1},
		},
	})

	want := []string{"another-future-model", "some-future-model"}
	if !slices.Equal(projection.UnpricedModels, want) {
		t.Fatalf("unpriced models = %v, want %v (deduplicated, sorted, no pseudo-model)", projection.UnpricedModels, want)
	}
}

func TestTotalsByDeliveryNamesNoModelWhenEverythingPriced(t *testing.T) {
	store := newTelemetryStore(t)
	catalog := NewCatalog([]ModelRate{{
		Provider: "test", Model: "test-model", EffectiveAt: time.Unix(0, 0),
		InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD",
	}})
	priced := NewStore(store.db, WithCatalog(catalog))
	s := mustBegin(t, priced, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})

	projection := mustSnapshot(t, priced, SnapshotRequest{
		SessionID: s.ID, SourceID: "main", Sequence: 1,
		ModelUsage: []ModelUsage{{Model: "test-model", InputTokens: 1}},
	})
	if len(projection.UnpricedModels) != 0 {
		t.Fatalf("unpriced models = %v, want none", projection.UnpricedModels)
	}
}

func TestGetSnapshotReturnsNilForAnAbsentBaseline(t *testing.T) {
	store := newTelemetryStore(t)
	s := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})
	got, err := store.GetSnapshot(context.Background(), s.ID, "main")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if got != nil {
		t.Fatalf("GetSnapshot = %+v, want nil for a baseline that was never ingested", got)
	}
}

func TestGetSnapshotReflectsLatestIngestedCounters(t *testing.T) {
	store := newTelemetryStore(t)
	s := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "codex", ExternalSessionID: "thr-1"})
	mustSnapshot(t, store, SnapshotRequest{SessionID: s.ID, SourceID: "main", Sequence: 3, InputTokens: 5, OutputTokens: 7})

	got, err := store.GetSnapshot(context.Background(), s.ID, "main")
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if got == nil || got.Sequence != 3 || got.Counters.InputTokens != 5 || got.Counters.OutputTokens != 7 {
		t.Fatalf("GetSnapshot = %+v, want sequence=3 input=5 output=7", got)
	}
}
