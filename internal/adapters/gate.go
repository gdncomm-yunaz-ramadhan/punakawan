package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// caller is the subset of *Client's behavior Gate depends on, so tests can
// substitute a fake instead of spawning a real adapter subprocess.
type caller interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// Gate wraps an adapter Client. Call is the seam every ordinary read (and,
// historically, every write) went through; it now refuses any operation the
// adapter's own manifest declares side-effecting, because the only place
// authorized to actually perform a provider write is the durable outbox's
// execution path (internal/providerwrite), after that write has been
// validated and durably recorded. ExecuteWrite is that one execution path's
// seam - see its own doc comment for why it exists and why nothing else
// should call it.
type Gate struct {
	adapterID string
	manifest  protocol.AdapterManifest
	client    caller

	// schemaMu/schemas cache each operation's resolved input_schema the
	// first time it's needed, since resolving a JSON Schema is real work
	// that has no reason to repeat on every call to the same operation. Its
	// manifest was already validated at Registry.Gate time (every
	// input_schema resolves cleanly), so resolution here cannot fail in
	// practice - the error return only guards against a Gate constructed
	// directly (as tests do) with a manifest that skipped that validation.
	schemaMu sync.Mutex
	schemas  map[string]*jsonschema.Resolved
}

// NewGate constructs a Gate for an already-started adapter client and its
// manifest (as returned by the adapter's "initialize" call).
func NewGate(adapterID string, manifest protocol.AdapterManifest, client caller) *Gate {
	return &Gate{adapterID: adapterID, manifest: manifest, client: client}
}

// Call invokes op via the adapter's "execute" method for every operation
// this adapter's manifest declares read-only (side_effect: false). An
// undeclared operation, or one the manifest marks side_effect: true, is
// rejected before the adapter process is ever invoked: a side-effecting
// operation must be enqueued through a domain service (which validates it
// and durably records it in the outbox) rather than called directly, and an
// undeclared operation is simply not something this adapter offers.

// Manifest returns the authoritative manifest obtained from this adapter when
// the Gate was initialized. Callers must treat the returned value as read-only.
func (g *Gate) Manifest() protocol.AdapterManifest {
	return g.manifest
}

func (g *Gate) Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error) {
	metadata, ok := g.manifest.Operations[op]
	if !ok {
		return nil, fmt.Errorf("adapters: operation %q is not declared", op)
	}
	if metadata.SideEffect {
		return nil, fmt.Errorf("adapters: side-effecting operation %q must be enqueued through a domain service", op)
	}
	if err := g.validatePayload(op, params); err != nil {
		return nil, err
	}
	return g.rawCall(ctx, op, params)
}

// ExecuteWrite performs op directly against the adapter process, bypassing
// Call's side-effecting rejection. It exists solely for
// internal/providerwrite's worker to invoke once it has already claimed a
// durably enqueued, previously validated write intent - no other caller
// should ever reach for this method, since doing so would recreate exactly
// the unvalidated, unrecorded direct-write path Call now refuses. op must
// still be declared and side_effect: true; an undeclared operation is
// rejected the same as Call rejects it.
func (g *Gate) ExecuteWrite(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error) {
	metadata, ok := g.manifest.Operations[op]
	if !ok {
		return nil, fmt.Errorf("adapters: operation %q is not declared", op)
	}
	if !metadata.SideEffect {
		return nil, fmt.Errorf("adapters: operation %q is not side-effecting; call it through Call instead", op)
	}
	if err := g.validatePayload(op, params); err != nil {
		return nil, err
	}
	return g.rawCall(ctx, op, params)
}

// validatePayload rejects a call whose params do not match op's declared
// input_schema before the adapter process ever sees it - a malformed
// side-effecting call must fail closed here, not reach the provider and
// fail (or worse, partially succeed) in some adapter-specific way. op is
// assumed already looked up by the caller (Call/ExecuteWrite), so a missing
// entry here can only mean this exact function has a bug.
func (g *Gate) validatePayload(op string, params map[string]any) error {
	metadata, ok := g.manifest.Operations[op]
	if !ok {
		return fmt.Errorf("adapters: operation %q is not declared", op)
	}
	resolved, err := g.resolvedInputSchema(op, metadata.InputSchema)
	if err != nil {
		return fmt.Errorf("adapters: operation %q has an invalid input_schema: %w", op, err)
	}
	if err := resolved.Validate(params); err != nil {
		return fmt.Errorf("adapters: operation %q payload does not match its input_schema: %w", op, err)
	}
	return nil
}

// resolvedInputSchema returns op's cached resolved input_schema, resolving
// and caching it on first use.
func (g *Gate) resolvedInputSchema(op string, raw protocol.AdapterManifestOperationsValueInputSchema) (*jsonschema.Resolved, error) {
	g.schemaMu.Lock()
	defer g.schemaMu.Unlock()
	if resolved, ok := g.schemas[op]; ok {
		return resolved, nil
	}
	resolved, err := resolveInputSchema(raw)
	if err != nil {
		return nil, err
	}
	if g.schemas == nil {
		g.schemas = make(map[string]*jsonschema.Resolved)
	}
	g.schemas[op] = resolved
	return resolved, nil
}

// rawCall merges {"op": op} into params (matching the prototype adapter's
// execute(params) convention of dispatching on a top-level "op" field, see
// packages/adapter-sdk/src/prototypeAdapter.ts) and invokes it.
func (g *Gate) rawCall(ctx context.Context, op string, params map[string]any) (json.RawMessage, error) {
	merged := make(map[string]any, len(params)+1)
	for k, v := range params {
		merged[k] = v
	}
	merged["op"] = op
	raw, err := g.client.Call(ctx, "execute", merged)
	if err != nil {
		var tooLarge *ErrResponseTooLarge
		if errors.As(err, &tooLarge) {
			// Client has no notion of adapter id; attribute the bounded
			// diagnostic to this Gate's adapter and the exact operation that
			// overflowed, rather than the raw JSON-RPC method name "execute".
			attributed := *tooLarge
			attributed.AdapterID = g.adapterID
			attributed.Operation = op
			return nil, &attributed
		}
	}
	return raw, err
}
