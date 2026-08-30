package telemetry

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// ModelRate is one model's per-token price as of EffectiveAt. A snapshot
// stores the exact ModelRate it resolved at capture time (see
// priceSnapshot), so a later Catalog.Replace (e.g. from a future
// "punakawan pricing refresh" command) never retroactively changes an
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
// lock, so a future "punakawan pricing refresh" command can swap the whole
// rate set while requests are in flight.
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

// Replace atomically swaps the catalog's whole rate set. It is the seam a
// later "punakawan pricing refresh" command (not implemented yet - the
// plan names it only as a future replacement path) would call after
// fetching current rates from configured official provider sources.
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

func normalizeModelName(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func ptr(v float64) *float64 { return &v }

// installedRates is the catalog every new Store ships with by default
// (see InstalledCatalog) until a future "punakawan pricing refresh"
// command replaces it from configured official provider sources. These
// figures are an installer-seeded default, not a live price feed: treat
// them as illustrative starting rates, not a guarantee of current
// accuracy, and prefer an explicitly observed provider price
// (SnapshotRequest.ObservedCost) over this catalog whenever one is
// available.
var installedRates = []ModelRate{
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
		InputPerMillion:      15.00,
		OutputPerMillion:     75.00,
		CacheWritePerMillion: ptr(18.75),
		CacheReadPerMillion:  ptr(1.50),
		Currency:             "USD",
		SourceURL:            "https://docs.anthropic.com/en/docs/about-claude/pricing",
	},
	{
		Provider: "anthropic", Model: "claude-haiku-4-5",
		EffectiveAt:          time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		InputPerMillion:      0.80,
		OutputPerMillion:     4.00,
		CacheWritePerMillion: ptr(1.00),
		CacheReadPerMillion:  ptr(0.08),
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
