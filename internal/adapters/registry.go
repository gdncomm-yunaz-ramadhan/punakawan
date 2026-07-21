package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// defaultEnvAllowlist mirrors tools.DefaultEnvAllowlist: every spawned
// adapter process needs at least these to run node at all.
var defaultEnvAllowlist = []string{"PATH", "HOME", "LANG", "TMPDIR"}

// AdapterSpec describes how to spawn one adapter process.
type AdapterSpec struct {
	Command string
	Args    []string
	// EnvPassthrough lists additional environment variable names (beyond
	// defaultEnvAllowlist) to copy from this process's environment into the
	// spawned adapter's environment, if set - e.g. secrets like
	// ATLASSIAN_API_TOKEN. Only these names are copied; the adapter process
	// does not inherit the full parent environment, per §11.4/§15.2's
	// secret-lease philosophy.
	EnvPassthrough []string
}

// Registry lazily starts and memoizes one adapters.Client (wrapped in a
// Gate) per adapter id. Each adapter's manifest is discovered dynamically
// via a "capabilities" call (§5.3's required message list) rather than
// hardcoded on the Go side, so Go and the TypeScript adapter's declared
// capabilities cannot silently drift apart.
type Registry struct {
	specs     map[string]AdapterSpec
	approvals *approvals.Store

	mu      sync.Mutex
	clients map[string]*Client
	gates   map[string]*Gate
}

// NewRegistry constructs a Registry for the given adapter specs.
func NewRegistry(specs map[string]AdapterSpec, store *approvals.Store) *Registry {
	return &Registry{
		specs:     specs,
		approvals: store,
		clients:   make(map[string]*Client),
		gates:     make(map[string]*Gate),
	}
}

// Gate returns the memoized Gate for adapterID, starting the adapter
// process, fetching its manifest, and completing the initialize handshake
// on first use.
func (r *Registry) Gate(ctx context.Context, adapterID string) (*Gate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if g, ok := r.gates[adapterID]; ok {
		if !r.clients[adapterID].Dead() {
			return g, nil
		}
		// The previously-spawned process crashed (or otherwise exited): drop
		// the stale entries and fall through to respawn, rather than handing
		// back a Gate whose Client can never respond again.
		delete(r.gates, adapterID)
		delete(r.clients, adapterID)
	}

	spec, ok := r.specs[adapterID]
	if !ok {
		return nil, fmt.Errorf("adapters: unknown adapter id %q", adapterID)
	}

	env := buildEnv(spec.EnvPassthrough)
	client, err := StartWithEnv(ctx, env, spec.Command, spec.Args...)
	if err != nil {
		return nil, fmt.Errorf("adapters: start %q: %w", adapterID, err)
	}

	manifest, err := fetchManifest(ctx, client)
	if err != nil {
		_ = client.Kill()
		return nil, fmt.Errorf("adapters: fetch capabilities for %q: %w", adapterID, err)
	}

	if _, err := client.Call(ctx, "initialize", manifest); err != nil {
		_ = client.Kill()
		return nil, fmt.Errorf("adapters: initialize %q: %w", adapterID, err)
	}

	gate := NewGate(adapterID, manifest, client, r.approvals)
	r.clients[adapterID] = client
	r.gates[adapterID] = gate
	return gate, nil
}

// fetchManifest calls the adapter's "capabilities" method (§5.3), which
// every real adapter implements by returning its own compiled-in manifest
// (see e.g. packages/adapter-atlassian/src/adapter.ts's initialize, which
// already validates that same manifest independently of whatever a caller
// sends it).
func fetchManifest(ctx context.Context, client *Client) (protocol.AdapterManifest, error) {
	raw, err := client.Call(ctx, "capabilities", nil)
	if err != nil {
		return protocol.AdapterManifest{}, err
	}
	var manifest protocol.AdapterManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return protocol.AdapterManifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return manifest, nil
}

// buildEnv resolves defaultEnvAllowlist plus extra against this process's
// actual environment, copying only variables that are set.
func buildEnv(extra []string) []string {
	names := make([]string, 0, len(defaultEnvAllowlist)+len(extra))
	names = append(names, defaultEnvAllowlist...)
	names = append(names, extra...)

	env := make([]string, 0, len(names))
	for _, name := range names {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// Close shuts down every adapter process this Registry has started.
func (r *Registry) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for id, c := range r.clients {
		if err := c.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("adapters: shutdown %q: %w", id, err)
		}
	}
	return firstErr
}
