package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// LiteLLMFeedURL is where current per-token prices for both Anthropic and
// OpenAI models are published in machine-readable form.
//
// Neither provider publishes one themselves: Anthropic's /v1/models and
// OpenAI's official OpenAPI schema both describe a model without ever
// naming its price, and their pricing pages are HTML with no data
// endpoint behind them. LiteLLM's file is MIT-licensed, updated many
// times a day, cites a source URL per entry, and is the only permissively
// licensed feed that carries Anthropic's separate 1-hour cache-write
// tier.
const LiteLLMFeedURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// ratesCacheFileName is the fetched feed, reduced to the models we can
// actually price, under the storage kernel's data directory.
const ratesCacheFileName = "model-rates.json"

// DefaultRatesTTL is how long a fetched feed is trusted before another
// fetch is attempted. Prices change on the order of months; a day keeps
// a new model's day-0 price within reach without making every hook
// invocation a network call.
const DefaultRatesTTL = 24 * time.Hour

// defaultRatesFetchTimeout bounds the fetch. A lifecycle hook runs in the
// foreground of someone's editor, so an unreachable feed has to fail fast
// and fall back rather than stall the client that invoked it.
const defaultRatesFetchTimeout = 5 * time.Second

// feedRateEffectiveAt is the effective date given to every rate parsed
// from the feed.
//
// The feed states current prices with no history, so there is no honest
// per-rate date to assign. Dating them "now" would be worse than
// useless: a snapshot captured an hour ago would resolve to nothing,
// because Catalog.Resolve only ever returns a rate effective at or before
// the moment being priced. Backdating them instead makes the feed apply
// to any snapshot, which costs nothing in accuracy - priceSnapshot
// records the exact ModelRate it resolved onto the snapshot itself, so no
// already-priced snapshot is ever re-costed by a later catalog change.
var feedRateEffectiveAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// feedProviders are the litellm_provider values worth importing. The feed
// covers dozens of providers and gateways; punakawan only ever sees usage
// reported by Claude Code and Codex.
var feedProviders = map[string]string{
	"anthropic":              "anthropic",
	"openai":                 "openai",
	"text-completion-openai": "openai",
}

// RatesFeed fetches current model prices and caches them on disk.
//
// It is deliberately not wired into NewStore. Constructing a Store must
// stay free of network access and of any read of this machine's real
// config directory, or every test that builds one acquires both; a
// composition root calls Prime once at startup instead.
type RatesFeed struct {
	// URL defaults to LiteLLMFeedURL.
	URL string
	// CachePath is where the reduced feed is stored. Empty means the
	// canonical path under the storage kernel's data directory.
	CachePath string
	// TTL defaults to DefaultRatesTTL.
	TTL time.Duration
	// Client defaults to a client bounded by defaultRatesFetchTimeout.
	Client *http.Client
	// Now defaults to time.Now, and exists so a test can age the cache
	// without sleeping.
	Now func() time.Time
}

// RatesOrigin says where the rates a Load returned actually came from.
type RatesOrigin string

const (
	// RatesFromNetwork - the feed was fetched and the cache rewritten.
	RatesFromNetwork RatesOrigin = "network"
	// RatesFromCache - the cache was still within its TTL.
	RatesFromCache RatesOrigin = "cache"
	// RatesFromStaleCache - the fetch failed and an expired cache was
	// used rather than losing every price the machine already knew.
	RatesFromStaleCache RatesOrigin = "stale-cache"
	// RatesFromInstalled - nothing was available, so only the compiled-in
	// table applies.
	RatesFromInstalled RatesOrigin = "installed"
)

// RatesStatus describes the outcome of a Load, for doctor to report. Err
// is set whenever a fetch was attempted and failed, even when Load still
// returned usable rates from a stale cache.
type RatesStatus struct {
	Origin    RatesOrigin
	CachePath string
	FetchedAt time.Time
	RateCount int
	SourceURL string
	Err       error
}

// ratesCache is the on-disk form: the feed reduced to what we can price,
// plus when it was fetched.
type ratesCache struct {
	FetchedAt time.Time   `json:"fetched_at"`
	SourceURL string      `json:"source_url"`
	Rates     []ModelRate `json:"rates"`
}

// RatesFeedURLOverrideEnv points the feed somewhere other than
// LiteLLMFeedURL, or - set to RatesFeedOff - stops it fetching at all.
// A test uses it to keep the network out of a process that primes the
// catalog; an operator behind a mirror can point it at their own copy.
const RatesFeedURLOverrideEnv = "PUNAKAWAN_RATES_FEED_URL"

// RatesFeedOff is the RatesFeedURLOverrideEnv value that disables
// fetching. An existing cache is still honoured, however old.
const RatesFeedOff = "off"

func (f *RatesFeed) url() string {
	if strings.TrimSpace(f.URL) != "" {
		return f.URL
	}
	if override := strings.TrimSpace(os.Getenv(RatesFeedURLOverrideEnv)); override != "" {
		return override
	}
	return LiteLLMFeedURL
}

func (f *RatesFeed) fetchDisabled() bool {
	return f.url() == RatesFeedOff
}

func (f *RatesFeed) ttl() time.Duration {
	if f.TTL > 0 {
		return f.TTL
	}
	return DefaultRatesTTL
}

func (f *RatesFeed) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now().UTC()
}

func (f *RatesFeed) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: defaultRatesFetchTimeout}
}

// Load returns the feed's rates, preferring a cache within its TTL, then
// a live fetch, then an expired cache. It never returns an error: a
// machine that cannot reach the feed must still price everything the
// installed table covers, so an unreachable feed degrades the catalog
// rather than failing whatever asked for it. The failure is reported in
// RatesStatus.Err for doctor to surface.
func (f *RatesFeed) Load(ctx context.Context) ([]ModelRate, RatesStatus) {
	path, pathErr := f.cachePath()
	status := RatesStatus{Origin: RatesFromInstalled, CachePath: path, SourceURL: f.url()}
	if pathErr != nil {
		status.Err = pathErr
		return nil, status
	}

	cached, cacheErr := readRatesCache(path)
	if cacheErr == nil && f.now().Sub(cached.FetchedAt) < f.ttl() {
		return cached.Rates, RatesStatus{
			Origin: RatesFromCache, CachePath: path, FetchedAt: cached.FetchedAt,
			RateCount: len(cached.Rates), SourceURL: cached.SourceURL,
		}
	}

	if f.fetchDisabled() {
		if cacheErr == nil {
			return cached.Rates, RatesStatus{
				Origin: RatesFromStaleCache, CachePath: path, FetchedAt: cached.FetchedAt,
				RateCount: len(cached.Rates), SourceURL: cached.SourceURL,
			}
		}
		return nil, status
	}

	rates, fetchErr := f.fetch(ctx)
	if fetchErr == nil {
		fresh := ratesCache{FetchedAt: f.now(), SourceURL: f.url(), Rates: rates}
		// A cache we cannot write is a slower startup next time, not a
		// reason to discard prices we just successfully fetched.
		writeErr := writeRatesCache(path, fresh)
		return rates, RatesStatus{
			Origin: RatesFromNetwork, CachePath: path, FetchedAt: fresh.FetchedAt,
			RateCount: len(rates), SourceURL: fresh.SourceURL, Err: writeErr,
		}
	}

	if cacheErr == nil {
		return cached.Rates, RatesStatus{
			Origin: RatesFromStaleCache, CachePath: path, FetchedAt: cached.FetchedAt,
			RateCount: len(cached.Rates), SourceURL: cached.SourceURL, Err: fetchErr,
		}
	}
	status.Err = fetchErr
	return nil, status
}

// Prime loads the feed and installs it over the process-wide default
// catalog every Store resolves against. A model the feed names replaces
// the compiled-in entry for it entirely; a model only the compiled-in
// table names is kept, so an offline machine loses nothing.
func (f *RatesFeed) Prime(ctx context.Context) RatesStatus {
	fetched, status := f.Load(ctx)
	if len(fetched) == 0 {
		return status
	}
	DefaultCatalog().Replace(MergeRates(installedRates, fetched))
	return status
}

// MergeRates layers fetched rates over installed ones. The feed is
// authoritative for any model it names - keeping a hand-typed rate
// alongside a fetched one would resurrect exactly the drift the feed
// exists to end - and the installed table remains the only source for a
// model the feed does not carry.
func MergeRates(installed, fetched []ModelRate) []ModelRate {
	covered := make(map[string]bool, len(fetched))
	for _, r := range fetched {
		covered[normalizeModelName(r.Model)] = true
	}
	merged := make([]ModelRate, 0, len(installed)+len(fetched))
	for _, r := range installed {
		if !covered[normalizeModelName(r.Model)] {
			merged = append(merged, r)
		}
	}
	return append(merged, fetched...)
}

func (f *RatesFeed) cachePath() (string, error) {
	if strings.TrimSpace(f.CachePath) != "" {
		return f.CachePath, nil
	}
	dir, err := storage.DataDir()
	if err != nil {
		return "", fmt.Errorf("telemetry: resolve rates cache dir: %w", err)
	}
	return filepath.Join(dir, ratesCacheFileName), nil
}

func (f *RatesFeed) fetch(ctx context.Context) ([]ModelRate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url(), nil)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build rates request: %w", err)
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("telemetry: fetch rates feed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telemetry: rates feed returned %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("telemetry: read rates feed: %w", err)
	}
	rates, err := ParseLiteLLMFeed(body)
	if err != nil {
		return nil, err
	}
	if len(rates) == 0 {
		return nil, errors.New("telemetry: rates feed named no priceable anthropic or openai model")
	}
	return rates, nil
}

// litellmEntry is the subset of a feed entry that carries price. Costs
// are per single token and are frequently written in scientific notation
// (5E-7), which encoding/json handles as a float.
type litellmEntry struct {
	Provider        string  `json:"litellm_provider"`
	InputPerToken   float64 `json:"input_cost_per_token"`
	OutputPerToken  float64 `json:"output_cost_per_token"`
	CacheReadToken  float64 `json:"cache_read_input_token_cost"`
	CacheWriteToken float64 `json:"cache_creation_input_token_cost"`
	Source          string  `json:"source"`
}

// ParseLiteLLMFeed reduces the feed to the Anthropic and OpenAI models
// punakawan can actually see reported.
//
// The feed keys every routing variant of a model separately -
// claude-opus-5, anthropic.claude-opus-5, azure_ai/claude-opus-5,
// bedrock/... - which is over three thousand keys for a few hundred real
// models. Only the canonical key is kept: the variants normalize to the
// same name anyway (see modelNamePrefixes), so importing them would be
// several thousand duplicate entries resolving to the same rate.
func ParseLiteLLMFeed(body []byte) ([]ModelRate, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("telemetry: decode rates feed: %w", err)
	}
	seen := make(map[string]bool, len(raw))
	rates := make([]ModelRate, 0, 256)
	for key, rawEntry := range raw {
		// sample_spec is the feed's own schema documentation, not a model.
		if key == "sample_spec" || strings.Contains(key, "/") || isPrefixedModelName(key) {
			continue
		}
		var entry litellmEntry
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			// A single malformed entry is the feed's problem, not a
			// reason to lose the other three thousand.
			continue
		}
		provider, ok := feedProviders[entry.Provider]
		if !ok {
			continue
		}
		if entry.InputPerToken <= 0 && entry.OutputPerToken <= 0 {
			continue
		}
		normalized := normalizeModelName(key)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true

		rate := ModelRate{
			Provider:         provider,
			Model:            key,
			EffectiveAt:      feedRateEffectiveAt,
			InputPerMillion:  perMillion(entry.InputPerToken),
			OutputPerMillion: perMillion(entry.OutputPerToken),
			Currency:         "USD",
			SourceURL:        entry.Source,
		}
		if rate.SourceURL == "" {
			rate.SourceURL = LiteLLMFeedURL
		}
		// A model that does not charge for cache writes or reads reports
		// no such field at all; leaving the pointer nil keeps "this
		// provider does not bill it" distinct from "it is free".
		if entry.CacheWriteToken > 0 {
			rate.CacheWritePerMillion = ptr(perMillion(entry.CacheWriteToken))
		}
		if entry.CacheReadToken > 0 {
			rate.CacheReadPerMillion = ptr(perMillion(entry.CacheReadToken))
		}
		rates = append(rates, rate)
	}
	return rates, nil
}

// perMillion scales a per-single-token price up to the per-million unit
// the catalog works in, rounding away the binary-float residue that
// scaling by a million leaves behind - 2E-7 becomes 0.19999999999999998
// otherwise, which is the same number but reads like a bug wherever it is
// printed or written to the cache.
func perMillion(perToken float64) float64 {
	return math.Round(perToken*1e6*1e8) / 1e8
}

// isPrefixedModelName reports whether key is a provider-routed variant of
// an id the feed also carries under its canonical name.
func isPrefixedModelName(key string) bool {
	lower := strings.ToLower(key)
	for _, prefix := range modelNamePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func readRatesCache(path string) (ratesCache, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ratesCache{}, err
	}
	var cache ratesCache
	if err := json.Unmarshal(body, &cache); err != nil {
		return ratesCache{}, fmt.Errorf("telemetry: decode rates cache %s: %w", path, err)
	}
	if len(cache.Rates) == 0 {
		return ratesCache{}, fmt.Errorf("telemetry: rates cache %s names no rate", path)
	}
	return cache, nil
}

func writeRatesCache(path string, cache ratesCache) error {
	body, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("telemetry: encode rates cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("telemetry: create rates cache dir: %w", err)
	}
	// Written through a temp file in the same directory so a reader never
	// observes a half-written cache, and a crash mid-write leaves the
	// previous one intact.
	tmp, err := os.CreateTemp(filepath.Dir(path), ratesCacheFileName+".*")
	if err != nil {
		return fmt.Errorf("telemetry: create rates cache temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("telemetry: write rates cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("telemetry: close rates cache temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("telemetry: chmod rates cache: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("telemetry: install rates cache: %w", err)
	}
	return nil
}
