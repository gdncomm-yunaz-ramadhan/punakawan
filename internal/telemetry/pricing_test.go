package telemetry

import (
	"testing"
	"time"
)

func TestCatalogResolvePicksLatestEffectiveDatedRate(t *testing.T) {
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	catalog := NewCatalog([]ModelRate{
		{Model: "widget", EffectiveAt: old, InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"},
		{Model: "widget", EffectiveAt: newer, InputPerMillion: 10, OutputPerMillion: 20, Currency: "USD"},
	})

	rate, ok := catalog.Resolve("widget", time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("Resolve: not found, want the older rate to apply")
	}
	if rate.InputPerMillion != 1 {
		t.Fatalf("input per million = %v, want the rate effective before the newer one supersedes it", rate.InputPerMillion)
	}

	rate, ok = catalog.Resolve("widget", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if !ok {
		t.Fatal("Resolve: not found, want the newer rate to apply")
	}
	if rate.InputPerMillion != 10 {
		t.Fatalf("input per million = %v, want the superseding rate", rate.InputPerMillion)
	}
}

func TestCatalogResolveBeforeAnyEffectiveDateIsUnknown(t *testing.T) {
	catalog := NewCatalog([]ModelRate{
		{Model: "widget", EffectiveAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"},
	})
	if _, ok := catalog.Resolve("widget", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)); ok {
		t.Fatal("Resolve: found a rate before its own effective date")
	}
}

func TestCatalogResolveUnknownModelReturnsNotFound(t *testing.T) {
	catalog := NewCatalog([]ModelRate{{Model: "widget", EffectiveAt: time.Unix(0, 0), InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"}})
	if _, ok := catalog.Resolve("does-not-exist", time.Now()); ok {
		t.Fatal("Resolve: found a rate for a model the catalog never named")
	}
}

func TestCatalogResolveIsCaseAndSpaceInsensitive(t *testing.T) {
	catalog := NewCatalog([]ModelRate{{Model: "Claude-Sonnet-4-5", EffectiveAt: time.Unix(0, 0), InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"}})
	if _, ok := catalog.Resolve("  claude-sonnet-4-5  ", time.Now()); !ok {
		t.Fatal("Resolve: expected a case/space-insensitive match")
	}
}

func TestInstalledCatalogResolvesSeededModels(t *testing.T) {
	catalog := InstalledCatalog()
	for _, model := range []string{"claude-sonnet-4-5", "claude-opus-4-5", "claude-haiku-4-5", "gpt-4o", "gpt-4o-mini"} {
		if _, ok := catalog.Resolve(model, time.Now()); !ok {
			t.Errorf("installed catalog has no rate for seeded model %q", model)
		}
	}
}

func TestCatalogReplaceSwapsRatesWithoutAffectingAlreadyCapturedSnapshots(t *testing.T) {
	catalog := NewCatalog([]ModelRate{{Model: "widget", EffectiveAt: time.Unix(0, 0), InputPerMillion: 1, OutputPerMillion: 2, Currency: "USD"}})
	rate, ok := catalog.Resolve("widget", time.Now())
	if !ok || rate.InputPerMillion != 1 {
		t.Fatalf("Resolve before Replace = %+v, %v", rate, ok)
	}
	captured := rate // a snapshot would have stored this value already

	catalog.Replace([]ModelRate{{Model: "widget", EffectiveAt: time.Unix(0, 0), InputPerMillion: 99, OutputPerMillion: 2, Currency: "USD"}})
	if captured.InputPerMillion != 1 {
		t.Fatalf("a value already captured by a prior Resolve changed after Replace: %+v", captured)
	}
	rate, ok = catalog.Resolve("widget", time.Now())
	if !ok || rate.InputPerMillion != 99 {
		t.Fatalf("Resolve after Replace = %+v, %v, want the replaced rate", rate, ok)
	}
}
