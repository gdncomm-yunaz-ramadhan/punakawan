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
	for _, model := range []string{
		"claude-opus-5", "claude-opus-4-8", "claude-sonnet-5", "claude-sonnet-4-6", "claude-fable-5-1",
		"claude-sonnet-4-5", "claude-opus-4-5", "claude-haiku-4-5", "gpt-4o", "gpt-4o-mini",
	} {
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

// The delivery that prompted this ran entirely on claude-opus-5 and
// recorded 6.3M tokens, but every snapshot priced unknown because the
// catalog stopped at claude-opus-4-5 - so the whole delivery reported a
// nil cost and a permanently incomplete telemetry status.
func TestInstalledCatalogPricesTheModelsClientsActuallyReport(t *testing.T) {
	catalog := InstalledCatalog()
	for _, model := range []string{"claude-opus-5", "claude-sonnet-5"} {
		rate, ok := catalog.Resolve(model, time.Now())
		if !ok {
			t.Fatalf("no rate for %q", model)
		}
		if rate.InputPerMillion <= 0 || rate.OutputPerMillion <= 0 || rate.Currency == "" {
			t.Errorf("%q resolved to an unusable rate %+v", model, rate)
		}
	}
}

func TestCatalogResolveStripsDateSuffixAndProviderPrefix(t *testing.T) {
	catalog := InstalledCatalog()
	for _, model := range []string{
		"claude-haiku-4-5-20251001",
		"us.anthropic.claude-opus-5",
		"anthropic/claude-sonnet-5",
		"US.Anthropic.Claude-Opus-5-20260101",
	} {
		if _, ok := catalog.Resolve(model, time.Now()); !ok {
			t.Errorf("Resolve(%q): expected the catalog to see through the prefix/date stamp", model)
		}
	}
}

func TestCatalogResolveStillRejectsAGenuinelyUnknownModel(t *testing.T) {
	catalog := InstalledCatalog()
	// Normalization must not be so eager that an unrecognised model
	// silently borrows a neighbouring model's price.
	for _, model := range []string{"claude-opus-99", "gpt-6", "claude-opus"} {
		if rate, ok := catalog.Resolve(model, time.Now()); ok {
			t.Errorf("Resolve(%q) unexpectedly matched %+v", model, rate)
		}
	}
}

func TestNonBillableModelRecognisesClientPseudoModels(t *testing.T) {
	for _, name := range []string{"<synthetic>", "  <SYNTHETIC>  ", ""} {
		if !NonBillableModel(name) {
			t.Errorf("NonBillableModel(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"claude-opus-5", "gpt-4o"} {
		if NonBillableModel(name) {
			t.Errorf("NonBillableModel(%q) = true, want false", name)
		}
	}
}
