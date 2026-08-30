package providerwrite

import (
	"context"
	"time"

	"github.com/ygrip/punakawan/internal/outbox"
)

// synchronousLease is deliberately short: ExecuteNow claims and resolves an
// intent inline, in the same call, so its claim never needs to survive
// longer than one attempt's own adapter round trip.
const synchronousLease = 2 * time.Minute

// ExecuteNow enqueues intent (idempotent by its OperationFingerprint, same
// as Enqueue) and, if it is not already terminal, immediately attempts to
// claim and resolve exactly that intent inline before returning - the
// synchronous counterpart to Pool's background loop, for the few call sites
// (a GitHub PR publish, a PR review submission, a worklog retry) whose
// existing contract returns the write's outcome directly rather than
// polling a durable row later.
//
// If something else (most likely the background Pool) claims the intent
// first, ExecuteNow does not wait or retry claiming - it returns the
// intent's current durable state, whatever that is. The write itself is
// never lost or duplicated either way: it is durably enqueued exactly once
// regardless of who ends up executing it.
func ExecuteNow(ctx context.Context, store *outbox.Store, resolver gateResolver, workerID string, intent outbox.Intent) (outbox.Intent, error) {
	enqueued, err := store.Enqueue(ctx, intent)
	if err != nil {
		return outbox.Intent{}, err
	}
	if enqueued.Status == outbox.StatusSucceeded || enqueued.Status == outbox.StatusCancelled {
		return enqueued, nil
	}

	claimed, err := store.ClaimByID(ctx, enqueued.ID, workerID, time.Now(), synchronousLease)
	if err != nil {
		return outbox.Intent{}, err
	}
	if claimed == nil {
		return store.Get(ctx, enqueued.ID)
	}

	w := &Worker{ID: workerID, Store: store, Adapters: resolver, LeaseTime: synchronousLease}
	w.resolve(ctx, *claimed)
	return store.Get(ctx, enqueued.ID)
}
