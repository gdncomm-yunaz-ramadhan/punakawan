package telemetry

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// ModelRate is one model's per-token price as of EffectiveAt. A snapshot
// stores the exact ModelRate it resolved at capture time (see
// priceSnapshot), so a later Catalog.Replace - RatesFeed.Prime installing
// freshly fetched rates, say - never retroactively changes an
// already-recorded snapshot's cost.
type ModelRate struct {
	Provider             string    `json:"provider"`
	Model                string    `json:"model"`
	EffectiveAt          time.Time `json:"effective_at"`
	InputPerMillion      float64   `json:"input_per_million"`
	OutputPerMillion     float64   `json:"output_per_million"`
	CacheWritePerMillion *float64  `json:"cache_write_per_million,omitempty"`
	CacheReadPerMillion  *float64  `json:"cache_read_per_million,omitempty"`
	Currency             string    `json:"currency"`
	SourceURL            string    `json:"source_url"`
}

// Catalog resolves a model name to its effective-dated rate. It is safe
// for concurrent use: Resolve takes a read lock, Replace takes a write
// lock, so RatesFeed.Prime can swap the whole rate set while requests are
// in flight.
type Catalog struct {
	mu    sync.RWMutex
	rates map[string][]ModelRate
}

// NewCatalog builds a Catalog from rates, grouped and sorted by effective
// date per normalized model name.
func NewCatalog(rates []ModelRate) *Catalog {
	c := &Catalog{}
	c.Replace(rates)
	return c
}

// Replace atomically swaps the catalog's whole rate set. RatesFeed.Prime
// calls it after fetching current rates, which is how a model released
// since this binary was built gets priced without a rebuild.
func (c *Catalog) Replace(rates []ModelRate) {
	grouped := make(map[string][]ModelRate, len(rates))
	for _, r := range rates {
		key := normalizeModelName(r.Model)
		grouped[key] = append(grouped[key], r)
	}
	for key := range grouped {
		sort.Slice(grouped[key], func(i, j int) bool {
			return grouped[key][i].EffectiveAt.Before(grouped[key][j].EffectiveAt)
		})
	}
	c.mu.Lock()
	c.rates = grouped
	c.mu.Unlock()
}

// Resolve returns the latest rate for model effective at or before at.
// ok is false when the catalog names no rate at all for model, or every
// rate it does name only becomes effective after at - in both cases the
// caller must treat this model's cost as explicitly unknown, never zero.
func (c *Catalog) Resolve(model string, at time.Time) (ModelRate, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	candidates := c.rates[normalizeModelName(model)]
	var best *ModelRate
	for i := range candidates {
		if candidates[i].EffectiveAt.After(at) {
			break
		}
		best = &candidates[i]
	}
	if best == nil {
		return ModelRate{}, false
	}
	return *best, true
}

// NonBillableModel reports whether name is a pseudo-model a client uses to
// account for tokens no provider charges for. Claude Code writes
// "<synthetic>" for locally generated messages; billing it as an unknown
// model would mark every snapshot that contains one unpriced, which is how
// a whole delivery's cost silently became nil.
func NonBillableModel(name string) bool {
	trimmed := normalizeModelName(name)
	return trimmed == "" || (strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">"))
}

// modelNamePrefixes are the routing prefixes a provider puts in front of an
// otherwise ordinary model id. They carry no pricing information of their
// own, so they are stripped before lookup rather than duplicated as extra
// catalog keys.
var modelNamePrefixes = []string{
	"us.anthropic.", "eu.anthropic.", "apac.anthropic.", "anthropic.",
	"anthropic/", "openai/",
}

// datedModelSuffix matches the -YYYYMMDD release stamp a provider appends
// to a model id (claude-haiku-4-5-20251001). The stamp names a snapshot of
// the same model at the same price, so it is stripped before lookup - the
// alternative is a catalog that goes stale every time a model is re-cut.
var datedModelSuffix = regexp.MustCompile(`-\d{8}$`)

func normalizeModelName(model string) string {
	name := strings.ToLower(strings.TrimSpace(model))
	for _, prefix := range modelNamePrefixes {
		if strings.HasPrefix(name, prefix) {
			name = strings.TrimPrefix(name, prefix)
			break
		}
	}
	return datedModelSuffix.ReplaceAllString(name, "")
}

func ptr(v float64) *float64 { return &v }

// installedRates is the offline fallback: the rates a process prices with
// before, or instead of, RatesFeed.Prime fetching current ones. The feed
// is authoritative for every model it names (see MergeRates), so these
// figures matter only for a machine that cannot reach it and for a model
// the feed does not carry.
//
// Being hand-maintained, this table drifts - claude-haiku-4-5 sat here at
// $0.80/$4.00 against a published $1.00/$5.00, and claude-opus-4-5 at
// $15/$75 against a published $5/$25, which is the whole reason the feed
// exists. Prefer an explicitly observed provider price
// (SnapshotRequest.ObservedCost) over this catalog whenever one is
// available.
//
// Cache rates follow Anthropic's published multipliers where the pricing
// page states them per-model: a 5-minute cache write costs 1.25x input and
// a cache read 0.1x input.
var installedRates = []ModelRate{
	{
		Provider: "anthropic", Model: "claude-opus-5",
		EffectiveAt:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:      5.00,
		OutputPerMillion:     25.00,
		CacheWritePerMillion: ptr(6.25),
		CacheReadPerMillion:  ptr(0.50),
		Currency:             "USD",
		SourceURL:            "https://docs.anthropic.com/en/docs/about-claude/pricing",
	},
	{
		Provider: "anthropic", Model: "claude-opus-4-8",
		EffectiveAt:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:      5.00,
		OutputPerMillion:     25.00,
		CacheWritePerMillion: ptr(6.25),
		CacheReadPerMillion:  ptr(0.50),
		Currency:             "USD",
		SourceURL:            "https://docs.anthropic.com/en/docs/about-claude/pricing",
	},
	{
		Provider: "anthropic", Model: "claude-sonnet-5",
		EffectiveAt:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:      2.00,
		OutputPerMillion:     10.00,
		CacheWritePerMillion: ptr(2.50),
		CacheReadPerMillion:  ptr(0.20),
		Currency:             "USD",
		SourceURL:            "https://docs.anthropic.com/en/docs/about-claude/pricing",
	},
	{
		Provider: "anthropic", Model: "claude-sonnet-4-6",
		EffectiveAt:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:      3.00,
		OutputPerMillion:     15.00,
		CacheWritePerMillion: ptr(3.75),
		CacheReadPerMillion:  ptr(0.30),
		Currency:             "USD",
		SourceURL:            "https://docs.anthropic.com/en/docs/about-claude/pricing",
	},
	{
		Provider: "anthropic", Model: "claude-fable-5-1",
		EffectiveAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:  10.00,
		OutputPerMillion: 50.00,
		// Claude Fable 5.1 prices cache reads flat rather than as a
		// multiple of input, so this one is not 0.1x.
		CacheWritePerMillion: ptr(12.50),
		CacheReadPerMillion:  ptr(0.25),
		Currency:             "USD",
		SourceURL:            "https://docs.anthropic.com/en/docs/about-claude/pricing",
	},
	{
		Provider: "anthropic", Model: "claude-sonnet-4-5",
		EffectiveAt:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:      3.00,
		OutputPerMillion:     15.00,
		CacheWritePerMillion: ptr(3.75),
		CacheReadPerMillion:  ptr(0.30),
		Currency:             "USD",
		SourceURL:            "https://docs.anthropic.com/en/docs/about-claude/pricing",
	},
	{
		Provider: "anthropic", Model: "claude-opus-4-5",
		EffectiveAt:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:      5.00,
		OutputPerMillion:     25.00,
		CacheWritePerMillion: ptr(6.25),
		CacheReadPerMillion:  ptr(0.50),
		Currency:             "USD",
		SourceURL:            "https://docs.anthropic.com/en/docs/about-claude/pricing",
	},
	{
		Provider: "anthropic", Model: "claude-haiku-4-5",
		EffectiveAt:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:      1.00,
		OutputPerMillion:     5.00,
		CacheWritePerMillion: ptr(1.25),
		CacheReadPerMillion:  ptr(0.10),
		Currency:             "USD",
		SourceURL:            "https://docs.anthropic.com/en/docs/about-claude/pricing",
	},
	{
		Provider: "openai", Model: "gpt-4o",
		EffectiveAt:         time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:     2.50,
		OutputPerMillion:    10.00,
		CacheReadPerMillion: ptr(1.25),
		Currency:            "USD",
		SourceURL:           "https://openai.com/api/pricing/",
	},
	{
		Provider: "openai", Model: "gpt-4o-mini",
		EffectiveAt:         time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:     0.15,
		OutputPerMillion:    0.60,
		CacheReadPerMillion: ptr(0.075),
		Currency:            "USD",
		SourceURL:           "https://openai.com/api/pricing/",
	},
}

// InstalledCatalog returns a fresh Catalog seeded with installedRates.
func InstalledCatalog() *Catalog { return NewCatalog(installedRates) }

// defaultCatalog is the process-wide catalog every Store resolves against
// unless given its own with WithCatalog. It starts as the compiled-in
// table and is replaced in place by RatesFeed.Prime, which is what lets a
// composition root install fetched rates once without every Store
// constructor - and so every test that builds one - reaching for the
// network or this machine's config directory.
var defaultCatalog = InstalledCatalog()

// DefaultCatalog returns that shared catalog. Replace is safe to call on
// it while requests are in flight.
func DefaultCatalog() *Catalog { return defaultCatalog }
