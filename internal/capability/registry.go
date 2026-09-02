// Package capability provides the single runtime registry of invokable
// capabilities in Punakawan. Before this package there were two independent
// lists of "which capabilities exist": the tools actually registered on the
// MCP server (internal/mcpserver.registerTools) and a hand-maintained mirror
// (workflowdef.KnownMCPCapabilities). They had already drifted — the mirror
// listed 46 names while the server registered ~70 tools — so a workflow
// definition referencing a real-but-unmirrored tool failed validation for no
// good reason.
//
// The Registry is that single source of truth. It is populated from the
// authoritative sources described in the plan §4.3:
//
//   - MCP tool descriptors, recorded as each tool is registered on the server;
//   - adapter operation manifests, recorded when adapters load.
//
// Workflow validation consults the same Registry, so the set a definition is
// validated against can no longer drift from the set the server exposes. The
// registry is deterministic and holds no reasoning — it is a set with source
// provenance, nothing more.
package capability

import (
	"sort"
	"sync"
)

// Source records where a capability descriptor came from, so enforcement and
// diagnostics can distinguish an MCP tool from an adapter operation.
type Source string

const (
	// SourceMCP marks a capability registered as an MCP tool on the server.
	SourceMCP Source = "mcp"
	// SourceAdapter marks a capability contributed by an adapter manifest's
	// operation list.
	SourceAdapter Source = "adapter"
)

// Descriptor is one invokable capability. Name is the capability identifier a
// workflow step references (an MCP tool name, or an adapter operation id).
// Intent is an optional coarse classification (adapter operations may carry
// one; MCP tools usually leave it blank) used later by workflow selectors.
type Descriptor struct {
	Name   string
	Source Source
	Intent string
	// Mutates reports whether this capability is expected to change
	// persisted state. For an MCP tool it is derived from the tool's own
	// standard MCP annotation (mcp.Tool.Annotations.ReadOnlyHint) at
	// registration time - conservative by default: no annotation means
	// Mutates is true. A role whose ToolPolicy.ReadOnly is set may not call
	// a tool with Mutates true (internal/mcpserver's live enforcement).
	Mutates bool
}

// Registry is a concurrency-safe set of capability descriptors keyed by name.
// It is written once at startup (as tools register and adapters load) and read
// concurrently thereafter by workflow validation and, in a later phase, the
// call-boundary enforcement middleware.
type Registry struct {
	mu     sync.RWMutex
	byName map[string]Descriptor
}

// NewRegistry returns an empty registry ready to be populated.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Descriptor)}
}

// Add records a descriptor. A blank name is ignored. The first registration of
// a name wins: re-registering the same name (e.g. an adapter op that shares a
// name with a tool) does not clobber the original source provenance, which
// keeps the "who owns this capability" answer stable.
func (r *Registry) Add(d Descriptor) {
	if d.Name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byName[d.Name]; !exists {
		r.byName[d.Name] = d
	}
}

// Has reports whether name is a registered capability.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.byName[name]
	return ok
}

// Lookup returns the descriptor for name and whether it was found.
func (r *Registry) Lookup(name string) (Descriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.byName[name]
	return d, ok
}

// Len reports how many distinct capabilities are registered.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byName)
}

// Names returns every registered capability name, sorted, so callers building
// a workflowdef.CapabilitySet get a deterministic list.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byName))
	for name := range r.byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Descriptors returns every registered descriptor, sorted by name.
func (r *Registry) Descriptors() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Descriptor, 0, len(r.byName))
	for _, d := range r.byName {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
