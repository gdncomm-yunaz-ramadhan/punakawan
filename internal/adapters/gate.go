package adapters

import (
	"context"
	"encoding/json"
	"fmt"

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
func (g *Gate) Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error) {
	metadata, ok := g.manifest.Operations[op]
	if !ok {
		return nil, fmt.Errorf("adapters: operation %q is not declared", op)
	}
	if metadata.SideEffect {
		return nil, fmt.Errorf("adapters: side-effecting operation %q must be enqueued through a domain service", op)
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
	return g.rawCall(ctx, op, params)
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
	return g.client.Call(ctx, "execute", merged)
}
