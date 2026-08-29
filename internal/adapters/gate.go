package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/syncqueue"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// caller is the subset of *Client's behavior Gate depends on, so tests can
// substitute a fake instead of spawning a real adapter subprocess.
type caller interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// Gate wraps an adapter Client, invoking operations directly - execution is
// authorized (or not) by internal/policy, not gated on a pending approval.
type Gate struct {
	adapterID string
	manifest  protocol.AdapterManifest
	client    caller
	// syncQueue lazily resolves the store that records a write which fails,
	// for later retry (punokawan-nbz). nil by default, so a Gate built
	// without calling SetSyncQueue (every existing test does) keeps the
	// original behavior of simply returning the error. It is a provider
	// rather than a resolved store so a Gate never forces the shared
	// storage kernel open until a write actually fails.
	syncQueue func() (*syncqueue.Queue, error)
}

// NewGate constructs a Gate for an already-started adapter client and its
// manifest (as returned by the adapter's "initialize" call).
func NewGate(adapterID string, manifest protocol.AdapterManifest, client caller) *Gate {
	return &Gate{adapterID: adapterID, manifest: manifest, client: client}
}

// SetSyncQueue configures provider to record any write this Gate makes that
// fails, so it can be found and retried later (punokawan-nbz) instead of the
// failure only ever existing as the error returned from that one Call.
// provider is resolved lazily, only when a write actually fails.
func (g *Gate) SetSyncQueue(provider func() (*syncqueue.Queue, error)) {
	g.syncQueue = provider
}

// Call invokes op via the adapter's "execute" method. params is merged with
// {"op": op} before being sent, matching the prototype adapter's
// execute(params) convention of dispatching on a top-level "op" field (see
// packages/adapter-sdk/src/prototypeAdapter.ts).
func (g *Gate) Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error) {
	merged := make(map[string]any, len(params)+1)
	for k, v := range params {
		merged[k] = v
	}
	merged["op"] = op

	raw, err := g.client.Call(ctx, "execute", merged)
	if err != nil && g.syncQueue != nil {
		queue, qerr := g.syncQueue()
		if qerr != nil {
			return nil, fmt.Errorf("adapters: call %q failed (%w), and opening the sync queue to record it also failed: %v", op, err, qerr)
		}
		entryID := fmt.Sprintf("sync-%s-%s-%s", g.adapterID, op, issueKey(params))
		entry, qerr := queue.Enqueue(syncqueue.Entry{
			Id:           entryID,
			RunId:        runID,
			Adapter:      g.adapterID,
			Op:           op,
			Params:       params,
			IssueIdOrKey: issueKey(params),
			Error:        err.Error(),
			CreatedAt:    time.Now().UTC(),
		})
		if qerr != nil {
			return nil, fmt.Errorf("adapters: call %q failed (%w), and recording it for retry also failed: %v", op, err, qerr)
		}
		return nil, fmt.Errorf("adapters: call %q failed: %w (recorded for retry as %q, attempt %d; use list_jira_sync_queue/retry_jira_sync_entry)", op, err, entry.Id, entry.Attempts)
	}
	return raw, err
}

// issueKey extracts the conventional "issueIdOrKey" parameter most Jira
// adapter operations take, for sync-queue conflict detection. Operations
// with no such parameter (or a non-string value) get an empty key, which
// still lets Enqueue detect a conflict against another entry that also has
// no issue key, just not one scoped to a specific issue.
func issueKey(params map[string]any) string {
	key, _ := params["issueIdOrKey"].(string)
	return key
}
