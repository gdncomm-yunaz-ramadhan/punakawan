package mcpserver

import (
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/capability"
)

// toolIndex extends capability.Registry (embedded, so every existing
// reg.Add/Has/Lookup/Names/Descriptors call in this package keeps working
// unchanged) with what find_tool needs to reveal a tool the default facade
// doesn't register up front: each tool's description, for keyword matching,
// and a closure that (re-)registers it live on the *mcp.Server it was built
// against. capability.Registry itself stays a pure, reasoning-free set (its
// own doc comment's contract); the live-registration mechanics live here,
// one layer up, in the package that actually owns the server.
type toolIndex struct {
	*capability.Registry

	mu       sync.Mutex
	describe map[string]string
	reAdd    map[string]func()
	live     map[string]bool
}

func newToolIndex() *toolIndex {
	return &toolIndex{
		Registry: capability.NewRegistry(),
		describe: make(map[string]string),
		reAdd:    make(map[string]func()),
		live:     make(map[string]bool),
	}
}

// toolIndexFrom wraps an already-populated capability.Registry (e.g.
// CapabilityRegistry's output) as a *toolIndex for a caller that only needs
// the embedded Registry's Names()/Has() lookups, never find_tool's
// reveal/re-register mechanics.
func toolIndexFrom(reg *capability.Registry) *toolIndex {
	return &toolIndex{
		Registry: reg,
		describe: make(map[string]string),
		reAdd:    make(map[string]func()),
		live:     make(map[string]bool),
	}
}

// record stashes a tool's description and its live-registration closure,
// and marks it live - every call to addTool passes through here, so at the
// point record runs the tool has, in fact, just been registered live.
func (idx *toolIndex) record(name, description string, register func()) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.describe[name] = description
	idx.reAdd[name] = register
	idx.live[name] = true
}

// hideAllExcept marks every recorded name outside keep as not live and
// removes it from server - called once, after every tool has registered
// and recorded itself, to shrink the server's default discovery surface
// down to the facade in keep. A name find_tool later reveals is re-added
// via its own stored closure, not by calling this again.
func (idx *toolIndex) hideAllExcept(server *mcp.Server, keep map[string]bool) {
	idx.mu.Lock()
	var toHide []string
	for name := range idx.describe {
		if keep[name] {
			continue
		}
		idx.live[name] = false
		toHide = append(toHide, name)
	}
	idx.mu.Unlock()
	server.RemoveTools(toHide...)
}

// toolMatch is one find_tool result.
type toolMatch struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	NewlyLive   bool   `json:"newly_live"`
}

// find returns every recorded tool matching query, live-registering (via
// each match's own stored closure) any that were not already live, capped
// at maxResults. Matching is case-insensitive substring on name or
// description, except a "select:a,b,c" query, which matches those exact
// names only (mirroring the same select-by-name convention this session's
// own host tool search supports).
func (idx *toolIndex) find(query string, maxResults int) []toolMatch {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	var names []string
	if rest, ok := strings.CutPrefix(query, "select:"); ok {
		for _, n := range strings.Split(rest, ",") {
			n = strings.TrimSpace(n)
			if _, ok := idx.describe[n]; ok {
				names = append(names, n)
			}
		}
	} else {
		q := strings.ToLower(strings.TrimSpace(query))
		for name, desc := range idx.describe {
			if strings.Contains(strings.ToLower(name), q) || strings.Contains(strings.ToLower(desc), q) {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	if maxResults > 0 && len(names) > maxResults {
		names = names[:maxResults]
	}

	out := make([]toolMatch, 0, len(names))
	for _, name := range names {
		newly := !idx.live[name]
		if newly {
			idx.reAdd[name]()
			idx.live[name] = true
		}
		out = append(out, toolMatch{Name: name, Description: idx.describe[name], NewlyLive: newly})
	}
	return out
}
