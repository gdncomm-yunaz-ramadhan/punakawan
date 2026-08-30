package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// punakawanAdapterProtocol is the only protocol version this process knows
// how to speak. protocol.AdapterManifest's own JSON decoding already rejects
// any other value; validateManifest re-checks it so a manifest built without
// going through that decode path (or a future decode-side relaxation) still
// cannot slip past initialization.
const punakawanAdapterProtocol = "punakawan.adapter/v1"

// defaultEnvAllowlist mirrors tools.DefaultEnvAllowlist: every spawned
// adapter process needs at least these to run node at all.
var defaultEnvAllowlist = []string{"PATH", "HOME", "LANG", "TMPDIR"}

// AdapterSpec describes how to spawn one adapter process.
type AdapterSpec struct {
	Command string
	Args    []string
	// Env contains trusted fixed NAME=value entries supplied by Punakawan
	// itself (for example the discovered workspace root). User secrets remain
	// governed by EnvPassthrough below.
	Env []string
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
	specs map[string]AdapterSpec

	mu      sync.Mutex
	clients map[string]*Client
	gates   map[string]*Gate
}

// NewRegistry constructs a Registry for the given adapter specs.
func NewRegistry(specs map[string]AdapterSpec) *Registry {
	return &Registry{
		specs:   specs,
		clients: make(map[string]*Client),
		gates:   make(map[string]*Gate),
	}
}

// Specs returns a copy of the configured adapter ids and specs, without
// starting any of them. Callers that only need to introspect what is
// configured (e.g. a panel health check) should use this rather than
// Gate, which has the side effect of spawning the adapter process.
func (r *Registry) Specs() map[string]AdapterSpec {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make(map[string]AdapterSpec, len(r.specs))
	for id, spec := range r.specs {
		out[id] = spec
	}
	return out
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

	env := append(buildEnv(spec.EnvPassthrough), spec.Env...)
	client, err := StartWithEnv(ctx, env, spec.Command, spec.Args...)
	if err != nil {
		return nil, fmt.Errorf("adapters: start %q: %w", adapterID, err)
	}

	manifest, err := fetchManifest(ctx, client)
	if err != nil {
		_ = client.Kill()
		return nil, fmt.Errorf("adapters: fetch capabilities for %q: %w", adapterID, err)
	}

	if err := validateManifest(adapterID, manifest, spec); err != nil {
		_ = client.Kill()
		return nil, fmt.Errorf("adapters: invalid manifest from %q: %w", adapterID, err)
	}

	if _, err := client.Call(ctx, "initialize", manifest); err != nil {
		_ = client.Kill()
		return nil, fmt.Errorf("adapters: initialize %q: %w", adapterID, err)
	}

	gate := NewGate(adapterID, manifest, client)
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

// validateManifest checks the contract a manifest must satisfy before this
// process will initialize an adapter with it: it must actually be the
// adapter this Registry asked for, every operation must document itself
// well enough for a caller to build a valid payload, and every secret it
// asks for must be one this host has explicitly agreed to hand it - never
// discovered only once a write is already in flight.
func validateManifest(adapterID string, manifest protocol.AdapterManifest, spec AdapterSpec) error {
	if manifest.Protocol != punakawanAdapterProtocol {
		return fmt.Errorf("protocol %q does not match %q", manifest.Protocol, punakawanAdapterProtocol)
	}
	if manifest.Id != adapterID {
		return fmt.Errorf("manifest id %q does not match configured adapter id %q", manifest.Id, adapterID)
	}
	if len(manifest.Operations) == 0 {
		return fmt.Errorf("manifest declares no operations")
	}
	for op, metadata := range manifest.Operations {
		if strings.TrimSpace(metadata.Description) == "" {
			return fmt.Errorf("operation %q has no description", op)
		}
		if _, err := resolveInputSchema(metadata.InputSchema); err != nil {
			return fmt.Errorf("operation %q has an invalid input_schema: %w", op, err)
		}
	}

	allowedSecrets := make(map[string]bool, len(spec.EnvPassthrough))
	for _, name := range spec.EnvPassthrough {
		allowedSecrets[name] = true
	}
	for _, secret := range manifest.Permissions.Secrets {
		if !allowedSecrets[secret] {
			return fmt.Errorf("declares secret %q that this host has not authorized for it", secret)
		}
	}
	return nil
}

// resolveInputSchema decodes and resolves an operation's declared
// input_schema, requiring it to describe a JSON object: every operation's
// call parameters are always a JSON object, never a scalar or array.
func resolveInputSchema(raw protocol.AdapterManifestOperationsValueInputSchema) (*jsonschema.Resolved, error) {
	encoded, err := json.Marshal(map[string]any(raw))
	if err != nil {
		return nil, fmt.Errorf("encode input_schema: %w", err)
	}
	var sch jsonschema.Schema
	if err := json.Unmarshal(encoded, &sch); err != nil {
		return nil, fmt.Errorf("decode input_schema: %w", err)
	}
	if sch.Type != "object" {
		return nil, fmt.Errorf(`input_schema must declare "type": "object", got %q`, sch.Type)
	}
	resolved, err := sch.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolve input_schema: %w", err)
	}
	return resolved, nil
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
