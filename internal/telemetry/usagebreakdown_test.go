package telemetry

import (
	"testing"
)

// A single summed figure cannot be checked against a bill: a cache read
// costs a fraction of an output token, and two sittings on one delivery
// can spend very differently. These are the two axes a reader asks about.
func TestTotalsByDeliveryBreaksDownByModelAndBySession(t *testing.T) {
	store := newTelemetryStore(t)
	first := mustBegin(t, store, BeginRequest{
		DeliveryID: "d1", ClientKind: "claude-code", ExternalSessionID: "ext-1", Participant: "semar",
	})
	second := mustBegin(t, store, BeginRequest{
		DeliveryID: "d1", ClientKind: "claude-code", ExternalSessionID: "ext-2", Participant: "gareng",
	})

	mustSnapshot(t, store, SnapshotRequest{
		SessionID: first.ID, SourceID: "main", Sequence: 1, ToolCalls: 4,
		ModelUsage: []ModelUsage{
			{Model: "claude-sonnet-5", InputTokens: 100, OutputTokens: 1_000, CacheWriteTokens: 2_000, CacheReadTokens: 30_000},
			{Model: "<synthetic>", InputTokens: 5},
		},
	})
	mustSnapshot(t, store, SnapshotRequest{
		SessionID: second.ID, SourceID: "main", Sequence: 1, ToolCalls: 6,
		ModelUsage: []ModelUsage{
			{Model: "claude-opus-5", InputTokens: 50, OutputTokens: 500, CacheWriteTokens: 1_000, CacheReadTokens: 10_000},
		},
	})

	totals := mustTotals(t, store, "d1")

	// The breakdowns must reconcile with the flat counters, or one of them
	// is lying about the same delivery.
	var modelInput, modelOutput, modelCacheWrite, modelCacheRead int64
	for _, m := range totals.ByModel {
		modelInput += m.InputTokens
		modelOutput += m.OutputTokens
		modelCacheWrite += m.CacheWriteTokens
		modelCacheRead += m.CacheReadTokens
	}
	if modelInput != totals.Counters.InputTokens || modelOutput != totals.Counters.OutputTokens ||
		modelCacheWrite != totals.Counters.CacheWriteTokens || modelCacheRead != totals.Counters.CacheReadTokens {
		t.Fatalf("by_model sums to %d/%d/%d/%d, want the counters %+v",
			modelInput, modelOutput, modelCacheWrite, modelCacheRead, totals.Counters)
	}

	var sessionTokens, sessionToolCalls int64
	seen := map[string]bool{}
	for _, s := range totals.BySession {
		sessionTokens += s.InputTokens + s.OutputTokens + s.CacheWriteTokens + s.CacheReadTokens
		sessionToolCalls += s.ToolCalls
		seen[s.SessionID] = true
	}
	if sessionTokens != totals.TotalTokens || sessionToolCalls != totals.Counters.ToolCalls {
		t.Fatalf("by_session sums to %d tokens / %d tool calls, want %d / %d",
			sessionTokens, sessionToolCalls, totals.TotalTokens, totals.Counters.ToolCalls)
	}
	if !seen[first.ID] || !seen[second.ID] {
		t.Fatalf("by_session = %+v, want both sessions named", totals.BySession)
	}

	// Per-model cost must be the delivery's cost, split - the two are
	// computed from the same rates and must not drift.
	var modelCost float64
	for _, m := range totals.ByModel {
		modelCost += m.EstimatedCost
		if m.Model == "claude-sonnet-5" && (m.CacheReadTokens != 30_000 || !m.Priced) {
			t.Errorf("sonnet entry = %+v, want its own cache reads and a price", m)
		}
	}
	if totals.EstimatedCost == nil {
		t.Fatal("estimated cost = nil, want both models priced")
	}
	if diff := modelCost - totals.EstimatedCost.Amount; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("by_model costs sum to %v, want the delivery's %v", modelCost, totals.EstimatedCost.Amount)
	}
}

// An unpriceable model must be visible as its own row rather than
// disappearing into a partial total.
func TestByModelMarksTheModelThatCouldNotBePriced(t *testing.T) {
	store := newTelemetryStore(t)
	session := mustBegin(t, store, BeginRequest{DeliveryID: "d1", ClientKind: "claude-code", ExternalSessionID: "ext-1"})
	mustSnapshot(t, store, SnapshotRequest{
		SessionID: session.ID, SourceID: "main", Sequence: 1,
		ModelUsage: []ModelUsage{{Model: "some-future-model", InputTokens: 10, OutputTokens: 20}},
	})

	totals := mustTotals(t, store, "d1")
	if len(totals.ByModel) != 1 {
		t.Fatalf("by_model = %+v, want the one model reported", totals.ByModel)
	}
	entry := totals.ByModel[0]
	if entry.Priced || entry.EstimatedCost != 0 {
		t.Fatalf("entry = %+v, want it marked unpriced with no fabricated cost", entry)
	}
	if entry.InputTokens != 10 || entry.OutputTokens != 20 {
		t.Fatalf("entry = %+v, want its tokens counted even though its cost is unknown", entry)
	}
}
