// Package timing provides a lightweight, per-request latency collector for
// the Punakawan Panel, per the performance plan §17 ("Add sub-probe
// timings" / "Add Server-Timing") and Phase 0's exit criterion that "every
// major latency source is measurable."
//
// A Collector accumulates named durations while a request is served; the
// server's timing middleware (dev-only, see internal/panel/server) reads it
// back as a W3C Server-Timing response header so a developer can see, in the
// browser's network panel, how long each sub-read of a handler took.
//
// Handlers and sources call the package-level Probe(ctx, name) helper
// unconditionally: when no Collector is attached to the context (the
// production default, or any request served with timing disabled) the probe
// is a no-op with effectively zero overhead, so instrumentation can live on
// the hot path permanently rather than behind a build tag.
package timing

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Collector accumulates named durations for a single request. It is safe for
// concurrent use: a handler may fan out sub-reads across goroutines and each
// may Probe/Record into the same Collector.
type Collector struct {
	mu sync.Mutex
	// durations sums elapsed time per name (repeated probes for the same
	// name add together, e.g. a per-workspace read looped N times).
	durations map[string]time.Duration
	// order preserves first-seen order so ServerTiming emits a stable,
	// meaningful sequence (the order the handler actually probed things)
	// rather than a map's random iteration order.
	order []string
}

// NewCollector returns an empty Collector ready to accept probes.
func NewCollector() *Collector {
	return &Collector{durations: make(map[string]time.Duration)}
}

// Record adds d to the total recorded under name, creating the entry (and
// remembering its insertion order) on first use. A negative d is ignored.
func (c *Collector) Record(name string, d time.Duration) {
	if c == nil || d < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.durations[name]; !ok {
		c.order = append(c.order, name)
	}
	c.durations[name] += d
}

// Probe starts timing name and returns a stop function that records the
// elapsed time when called. The idiom is:
//
//	defer c.Probe("workspace_list")()
//
// Repeated probes for the same name sum, so probing inside a loop reports the
// aggregate time spent in that section. A nil Collector yields a no-op stop
// function, so callers never need a nil check.
func (c *Collector) Probe(name string) func() {
	if c == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		c.Record(name, time.Since(start))
	}
}

// ServerTiming formats the collected durations as a W3C Server-Timing header
// value: "name;dur=NN,name2;dur=NN" where dur is milliseconds (matching the
// plan §17 example, which reports whole-millisecond dur values). Entries are
// emitted in first-probed (insertion) order for stable, readable output;
// names are sanitized to the token characters Server-Timing permits. Returns
// "" when nothing was recorded, so the caller can skip setting an empty
// header.
func (c *Collector) ServerTiming() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.order) == 0 {
		return ""
	}
	parts := make([]string, 0, len(c.order))
	for _, name := range c.order {
		d := c.durations[name]
		ms := float64(d) / float64(time.Millisecond)
		// strconv with -1 precision drops trailing zeros (e.g. "3" not
		// "3.000000"), keeping the header compact while retaining sub-ms
		// resolution for fast warm reads.
		dur := strconv.FormatFloat(ms, 'f', -1, 64)
		parts = append(parts, sanitizeName(name)+";dur="+dur)
	}
	return strings.Join(parts, ",")
}

// names returns the recorded metric names in insertion order (test helper /
// introspection).
func (c *Collector) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.order))
	copy(out, c.order)
	return out
}

// sanitizeName reduces name to the RFC 7230 token characters a Server-Timing
// metric name may use, replacing anything else with '_'. This keeps a stray
// space or comma in a probe name from producing a malformed header.
func sanitizeName(name string) string {
	if name == "" {
		return "_"
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// sortedNames returns the recorded names sorted; unused by ServerTiming (which
// preserves insertion order) but handy for tests that assert set membership
// without depending on probe order.
func (c *Collector) sortedNames() []string {
	out := c.names()
	sort.Strings(out)
	return out
}

// collectorKey is the unexported context key type for a Collector, avoiding
// collisions with any other package's context values.
type collectorKey struct{}

// WithCollector returns a copy of ctx carrying c, so downstream handlers and
// sources can retrieve it via FromContext / the package-level Probe.
func WithCollector(ctx context.Context, c *Collector) context.Context {
	return context.WithValue(ctx, collectorKey{}, c)
}

// FromContext returns the Collector attached to ctx, if any.
func FromContext(ctx context.Context) (*Collector, bool) {
	if ctx == nil {
		return nil, false
	}
	c, ok := ctx.Value(collectorKey{}).(*Collector)
	return c, ok && c != nil
}

// Probe is the package-level convenience the plan calls for: it starts a probe
// on the Collector in ctx (if one is attached) and returns its stop function,
// or a no-op stop when timing is disabled. Handlers and sources call it
// unconditionally:
//
//	defer timing.Probe(ctx, "workspace_list")()
func Probe(ctx context.Context, name string) func() {
	if c, ok := FromContext(ctx); ok {
		return c.Probe(name)
	}
	return func() {}
}
