package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// feedFixture is the shape the real LiteLLM file has, reduced to the
// cases that matter: an Anthropic model with both cache tiers, an OpenAI
// model with only a cache read, the routing variants of a model already
// present under its canonical key, a non-Claude/GPT provider, the feed's
// own schema entry, and a free model.
const feedFixture = `{
  "sample_spec": {"input_cost_per_token": 0.0, "litellm_provider": "one of the providers"},
  "claude-opus-5": {
    "litellm_provider": "anthropic",
    "input_cost_per_token": 0.000005,
    "output_cost_per_token": 0.000025,
    "cache_read_input_token_cost": 5E-7,
    "cache_creation_input_token_cost": 0.00000625,
    "cache_creation_input_token_cost_above_1hr": 0.00001,
    "source": "https://docs.anthropic.com/en/docs/about-claude/models/overview"
  },
  "anthropic.claude-opus-5": {"litellm_provider": "anthropic", "input_cost_per_token": 0.000005, "output_cost_per_token": 0.000025},
  "azure_ai/claude-opus-5": {"litellm_provider": "anthropic", "input_cost_per_token": 0.000005, "output_cost_per_token": 0.000025},
  "claude-haiku-4-5": {
    "litellm_provider": "anthropic",
    "input_cost_per_token": 0.000001,
    "output_cost_per_token": 0.000005,
    "cache_read_input_token_cost": 1E-7,
    "cache_creation_input_token_cost": 0.00000125
  },
  "gpt-5": {
    "litellm_provider": "openai",
    "input_cost_per_token": 0.00000125,
    "output_cost_per_token": 0.00001,
    "cache_read_input_token_cost": 1.25E-7
  },
  "gemini-3.8-flash": {"litellm_provider": "vertex_ai", "input_cost_per_token": 0.0000001, "output_cost_per_token": 0.0000004},
  "some-free-model": {"litellm_provider": "openai", "input_cost_per_token": 0.0, "output_cost_per_token": 0.0}
}`

func rateByModel(t *testing.T, rates []ModelRate, model string) ModelRate {
	t.Helper()
	for _, r := range rates {
		if r.Model == model {
			return r
		}
	}
	t.Fatalf("rates name no %q; got %d entries", model, len(rates))
	return ModelRate{}
}

func TestParseLiteLLMFeedConvertsPerTokenCostsToPerMillion(t *testing.T) {
	rates, err := ParseLiteLLMFeed([]byte(feedFixture))
	if err != nil {
		t.Fatalf("ParseLiteLLMFeed: %v", err)
	}

	opus := rateByModel(t, rates, "claude-opus-5")
	if opus.InputPerMillion != 5 || opus.OutputPerMillion != 25 {
		t.Errorf("opus rate = %v/%v per million, want 5/25", opus.InputPerMillion, opus.OutputPerMillion)
	}
	if opus.CacheWritePerMillion == nil || *opus.CacheWritePerMillion != 6.25 {
		t.Errorf("opus cache write = %v, want 6.25", opus.CacheWritePerMillion)
	}
	// Written 5E-7 in the feed; a decoder that cannot read scientific
	// notation would silently price every cache read at zero.
	if opus.CacheReadPerMillion == nil || *opus.CacheReadPerMillion != 0.5 {
		t.Errorf("opus cache read = %v, want 0.5", opus.CacheReadPerMillion)
	}
	if opus.SourceURL == "" || opus.Currency != "USD" || opus.Provider != "anthropic" {
		t.Errorf("opus provenance = %+v, want an anthropic USD rate citing its source", opus)
	}

	// OpenAI bills no cache write on this model, which is not the same as
	// billing zero for it.
	gpt := rateByModel(t, rates, "gpt-5")
	if gpt.CacheWritePerMillion != nil {
		t.Errorf("gpt-5 cache write = %v, want nil when the feed names no such cost", *gpt.CacheWritePerMillion)
	}
	if gpt.Provider != "openai" {
		t.Errorf("gpt-5 provider = %q, want openai", gpt.Provider)
	}
}

func TestParseLiteLLMFeedKeepsOnlyTheCanonicalPriceableEntries(t *testing.T) {
	rates, err := ParseLiteLLMFeed([]byte(feedFixture))
	if err != nil {
		t.Fatalf("ParseLiteLLMFeed: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rates {
		got[r.Model] = true
	}
	want := map[string]bool{"claude-opus-5": true, "claude-haiku-4-5": true, "gpt-5": true}
	for model := range want {
		if !got[model] {
			t.Errorf("rates are missing %q", model)
		}
	}
	// Routing variants normalize to a name already present, the schema
	// entry is not a model, a Vertex model is never reported to us, and a
	// zero-cost entry carries no price to install.
	for _, unwanted := range []string{"anthropic.claude-opus-5", "azure_ai/claude-opus-5", "sample_spec", "gemini-3.8-flash", "some-free-model"} {
		if got[unwanted] {
			t.Errorf("rates should not carry %q", unwanted)
		}
	}
	if len(rates) != len(want) {
		t.Errorf("rate count = %d, want %d: %+v", len(rates), len(want), got)
	}
}

// The whole point of the feed: a model the compiled-in table has never
// heard of has to price, and one the table names at a stale price has to
// be corrected rather than sitting alongside it.
func TestMergeRatesLetsTheFeedOverrideTheInstalledTable(t *testing.T) {
	fetched, err := ParseLiteLLMFeed([]byte(feedFixture))
	if err != nil {
		t.Fatalf("ParseLiteLLMFeed: %v", err)
	}
	catalog := NewCatalog(MergeRates(installedRates, fetched))
	at := time.Now().UTC()

	haiku, ok := catalog.Resolve("claude-haiku-4-5", at)
	if !ok {
		t.Fatal("claude-haiku-4-5 did not resolve")
	}
	if haiku.InputPerMillion != 1 || haiku.OutputPerMillion != 5 {
		t.Errorf("haiku rate = %v/%v, want the feed's 1/5, not the installed table's", haiku.InputPerMillion, haiku.OutputPerMillion)
	}

	// gpt-4o is in the installed table and not in this feed, so an
	// offline-era model must not disappear when the feed is layered on.
	if _, ok := catalog.Resolve("gpt-4o", at); !ok {
		t.Error("gpt-4o stopped resolving once feed rates were merged")
	}
	// A dated id must still normalize onto the fetched rate.
	if _, ok := catalog.Resolve("claude-opus-5-20260101", at); !ok {
		t.Error("a date-suffixed id did not resolve against a fetched rate")
	}
}

func newFeedServer(t *testing.T, body string) (*httptest.Server, *int) {
	t.Helper()
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func TestRatesFeedLoadFetchesThenServesFromCacheUntilTheTTLExpires(t *testing.T) {
	server, hits := newFeedServer(t, feedFixture)
	cachePath := filepath.Join(t.TempDir(), "model-rates.json")
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	feed := &RatesFeed{URL: server.URL, CachePath: cachePath, TTL: time.Hour, Now: func() time.Time { return now }}

	rates, status := feed.Load(context.Background())
	if status.Origin != RatesFromNetwork || status.Err != nil {
		t.Fatalf("first load = %s (%v), want a clean network fetch", status.Origin, status.Err)
	}
	if len(rates) != 3 || status.RateCount != 3 {
		t.Fatalf("first load returned %d rates, want 3", len(rates))
	}

	if _, status = feed.Load(context.Background()); status.Origin != RatesFromCache {
		t.Fatalf("second load = %s, want the cache", status.Origin)
	}
	if *hits != 1 {
		t.Errorf("feed was fetched %d times, want 1 while the cache is fresh", *hits)
	}

	now = now.Add(2 * time.Hour)
	if _, status = feed.Load(context.Background()); status.Origin != RatesFromNetwork {
		t.Fatalf("load past the TTL = %s, want a refetch", status.Origin)
	}
	if *hits != 2 {
		t.Errorf("feed was fetched %d times, want 2 once the cache aged out", *hits)
	}

	// The cache must be readable on its own, not just through Load.
	body, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var cache ratesCache
	if err := json.Unmarshal(body, &cache); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	if len(cache.Rates) != 3 || cache.SourceURL != server.URL {
		t.Errorf("cache = %d rates from %q, want 3 from the feed url", len(cache.Rates), cache.SourceURL)
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat cache: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache permissions = %v, want 0600", perm)
	}
}

// An unreachable feed must cost nothing: an expired cache is still every
// price this machine knows, and losing it would take the delivery cost
// back to null - the exact failure the feed exists to prevent.
func TestRatesFeedFallsBackToAStaleCacheWhenTheFetchFails(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "model-rates.json")
	server, _ := newFeedServer(t, feedFixture)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	warm := &RatesFeed{URL: server.URL, CachePath: cachePath, TTL: time.Hour, Now: func() time.Time { return now }}
	if _, status := warm.Load(context.Background()); status.Origin != RatesFromNetwork {
		t.Fatalf("warm-up load = %s, want a network fetch", status.Origin)
	}

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dead.Close()
	offline := &RatesFeed{
		URL: dead.URL, CachePath: cachePath, TTL: time.Hour,
		Now: func() time.Time { return now.Add(48 * time.Hour) },
	}
	rates, status := offline.Load(context.Background())
	if status.Origin != RatesFromStaleCache {
		t.Fatalf("offline load = %s, want the stale cache", status.Origin)
	}
	if len(rates) != 3 {
		t.Fatalf("offline load returned %d rates, want the 3 the cache holds", len(rates))
	}
	if status.Err == nil {
		t.Error("a stale-cache load must still report why the fetch failed, or doctor has nothing to show")
	}
}

func TestRatesFeedReportsInstalledOnlyWhenThereIsNoCacheAndNoNetwork(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer dead.Close()
	feed := &RatesFeed{URL: dead.URL, CachePath: filepath.Join(t.TempDir(), "model-rates.json")}

	rates, status := feed.Load(context.Background())
	if len(rates) != 0 || status.Origin != RatesFromInstalled {
		t.Fatalf("load = %d rates from %s, want none and installed-only", len(rates), status.Origin)
	}
	if status.Err == nil {
		t.Error("expected the fetch failure to be reported")
	}
}

// A machine that must not talk to the feed - a test, a sealed
// environment - still has to price everything it already knows.
func TestRatesFeedOffNeverFetchesButStillHonoursAnExistingCache(t *testing.T) {
	server, hits := newFeedServer(t, feedFixture)
	cachePath := filepath.Join(t.TempDir(), "model-rates.json")
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	if _, status := (&RatesFeed{URL: server.URL, CachePath: cachePath, TTL: time.Hour, Now: func() time.Time { return now }}).Load(context.Background()); status.Origin != RatesFromNetwork {
		t.Fatalf("warm-up load = %s, want a network fetch", status.Origin)
	}

	t.Setenv(RatesFeedURLOverrideEnv, RatesFeedOff)
	off := &RatesFeed{CachePath: cachePath, TTL: time.Hour, Now: func() time.Time { return now.Add(72 * time.Hour) }}
	rates, status := off.Load(context.Background())
	if len(rates) != 3 {
		t.Fatalf("load with fetching off returned %d rates, want the 3 the cache holds", len(rates))
	}
	if status.Err != nil {
		t.Errorf("status err = %v, want none - not fetching is a choice, not a failure", status.Err)
	}
	if *hits != 1 {
		t.Errorf("feed was fetched %d times, want 1 - the second load must not have gone out", *hits)
	}
}

// Prime is the only thing that touches the process-wide catalog, so it is
// also the only thing that has to leave it usable when the feed is
// unreachable.
func TestPrimeLeavesTheInstalledCatalogIntactWhenTheFeedIsUnreachable(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer dead.Close()
	feed := &RatesFeed{URL: dead.URL, CachePath: filepath.Join(t.TempDir(), "model-rates.json")}

	status := feed.Prime(context.Background())
	if status.Origin != RatesFromInstalled || status.Err == nil {
		t.Fatalf("prime = %s (%v), want installed-only with the failure reported", status.Origin, status.Err)
	}
	if _, ok := DefaultCatalog().Resolve("claude-opus-5", time.Now().UTC()); !ok {
		t.Error("the default catalog stopped resolving a compiled-in model after a failed prime")
	}
}
