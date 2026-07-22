package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/syncqueue"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// caller is the subset of *Client's behavior Gate depends on, so tests can
// substitute a fake instead of spawning a real adapter subprocess.
type caller interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// Gate wraps an adapter Client, enforcing the manifest-declared approval
// requirement (§5.4's operations[op].approval, §16's approval-gate model)
// before invoking any operation, per §13.2 "Apply policy before writes".
// This mirrors gitops.WorktreeManager's RequestApproval/Approve/Deny
// pattern, generalized from git worktrees to any adapter operation.
type Gate struct {
	adapterID string
	manifest  protocol.AdapterManifest
	client    caller
	approvals *approvals.Store
	// scopeMode is the workspace's policy.Approvals.Scope ("run" or "day");
	// empty behaves as "run", so a Gate built without calling
	// SetApprovalScope (every existing test does) keeps the original
	// per-run_id behavior unchanged.
	scopeMode string
	// syncQueue records a write that reaches the adapter (i.e. passed the
	// approval check) but fails, for later retry (punokawan-nbz). nil by
	// default, so a Gate built without calling SetSyncQueue (every existing
	// test does) keeps the original behavior of simply returning the error.
	syncQueue *syncqueue.Queue
}

// NewGate constructs a Gate for an already-started adapter client and its
// manifest (as returned by the adapter's "initialize" call). Approval scope
// defaults to per-run_id; call SetApprovalScope to widen it.
func NewGate(adapterID string, manifest protocol.AdapterManifest, client caller, store *approvals.Store) *Gate {
	return &Gate{adapterID: adapterID, manifest: manifest, client: client, approvals: store}
}

// SetApprovalScope sets how broad one human approval is for this Gate's
// adapter-write requests (policy.ApprovalsPolicy.Scope; punokawan-cy8).
func (g *Gate) SetApprovalScope(mode string) {
	g.scopeMode = mode
}

// SetSyncQueue configures q to record any write this Gate makes that fails
// after passing its approval check, so it can be found and retried later
// (punokawan-nbz) instead of the failure only ever existing as the error
// returned from that one Call.
func (g *Gate) SetSyncQueue(q *syncqueue.Queue) {
	g.syncQueue = q
}

// approvalID is scoped to a key, not the adapter or operation: one human
// approval covers every approval-required adapter write sharing that key. A
// different key still needs its own approval; the boundary is "whatever
// this key identifies may write through configured adapters", not "write
// anything forever".
func approvalID(key string) string {
	return fmt.Sprintf("approval-adapter-run-%s", key)
}

// scopeKey resolves runID to the actual key approvalID uses, honoring
// g.scopeMode. "day" shares one approval across every run against this
// adapter within a calendar UTC day (punokawan-cy8); anything else
// (including the zero value) keeps the original per-run_id behavior, so a
// Gate that never calls SetApprovalScope is unaffected.
func (g *Gate) scopeKey(runID string) string {
	if g.scopeMode == "day" {
		return fmt.Sprintf("%s-day-%s", g.adapterID, time.Now().UTC().Format("2006-01-02"))
	}
	return runID
}

// requiresApproval reports whether op is declared approval-required in the
// adapter's manifest. Operations the manifest doesn't mention at all, or
// declares without an approval requirement, are not gated.
func (g *Gate) requiresApproval(op string) bool {
	entry, ok := g.manifest.Operations[op]
	return ok && entry.Approval != nil && *entry.Approval == protocol.AdapterManifestOperationsValueApprovalRequired
}

// RequiresApproval reports whether the adapter manifest declares op as an
// approval-gated operation. MCP-facing orchestration uses this to decide
// whether it needs to ask the connected client to elicit human approval.
func (g *Gate) RequiresApproval(op string) bool {
	return g.requiresApproval(op)
}

// operationCategory maps an adapter operation name onto the closest
// protocol.ApprovalRecordOperation value. §16.1's categories don't
// enumerate "adapter operation" as its own concept, so this is an
// interpretive judgment call, mirroring gitops.WorktreeManager's own
// documented choice for worktree creation: operations whose name
// recognizably matches a specific category use it (confluence writes,
// issue creation/transition); anything else falls back to the general
// external-write category, since every approval-gated adapter operation is
// by definition a write to a system Punakawan doesn't own.
func operationCategory(op string) protocol.ApprovalRecordOperation {
	lower := strings.ToLower(op)
	switch {
	case strings.Contains(lower, "confluence"):
		return protocol.ApprovalRecordOperationConfluenceUpdate
	case strings.Contains(lower, "issue") && strings.Contains(lower, "create"):
		return protocol.ApprovalRecordOperationIssueCreation
	case strings.Contains(lower, "transition"):
		return protocol.ApprovalRecordOperationIssueTransition
	default:
		return protocol.ApprovalRecordOperationExternalWrite
	}
}

// RequestApproval creates a pending approval record covering every
// approval-required adapter operation this run performs, or
// returns the existing record if one was already requested (idempotent) -
// including when a different adapter or operation in the same run created
// it, since approval is scoped to the run, not the adapter or individual op
// (see approvalID). It is a no-op to call this for an operation the
// manifest does not require approval for; callers should check
// requiresApproval-equivalent behavior implicitly by simply calling Call,
// which only enforces the gate when the manifest asks for it.
func (g *Gate) RequestApproval(runID, op string, requestedBy protocol.ApprovalRecordRequestedBy) (protocol.ApprovalRecord, error) {
	if !g.requiresApproval(op) {
		return protocol.ApprovalRecord{}, nil
	}

	id := approvalID(g.scopeKey(runID))

	current, err := g.approvals.Current()
	if err != nil {
		return protocol.ApprovalRecord{}, err
	}
	if rec, ok := current[id]; ok {
		return rec, nil
	}

	target := "all configured adapters"
	reason := fmt.Sprintf("invoke approval-required adapter operations for run %q (first requested: %q on %q)", runID, op, g.adapterID)
	rec := protocol.ApprovalRecord{
		Id:          id,
		RunId:       runID,
		Operation:   operationCategory(op),
		Target:      &target,
		Reason:      &reason,
		RequestedBy: requestedBy,
		Status:      protocol.ApprovalRecordStatusPending,
		CreatedAt:   time.Now().UTC(),
	}
	if err := g.approvals.Append(rec); err != nil {
		return protocol.ApprovalRecord{}, err
	}
	return rec, nil
}

// Approve marks a pending adapter-write request as approved, covering every
// approval-required adapter operation this run performs.
func (g *Gate) Approve(runID, approvedBy string) error {
	return g.approvals.Resolve(approvalID(g.scopeKey(runID)), protocol.ApprovalRecordStatusApproved, approvedBy)
}

// Deny marks a pending adapter-write request as denied.
func (g *Gate) Deny(runID, approvedBy string) error {
	return g.approvals.Resolve(approvalID(g.scopeKey(runID)), protocol.ApprovalRecordStatusDenied, approvedBy)
}

// Call invokes op via the adapter's "execute" method, first checking that
// an approved request exists if the manifest declares op approval-required.
// params is merged with {"op": op} before being sent, matching the
// prototype adapter's execute(params) convention of dispatching on a top-
// level "op" field (see packages/adapter-sdk/src/prototypeAdapter.ts).
func (g *Gate) Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error) {
	if g.requiresApproval(op) {
		id := approvalID(g.scopeKey(runID))
		current, err := g.approvals.Current()
		if err != nil {
			return nil, err
		}
		rec, ok := current[id]
		if !ok || rec.Status != protocol.ApprovalRecordStatusApproved {
			return nil, fmt.Errorf("adapters: adapter %q is not approved for run %q (requested op %q, approval id %q); approve with `punakawan approvals approve %s --by <your-name>` and retry", g.adapterID, runID, op, id, id)
		}
	}

	merged := make(map[string]any, len(params)+1)
	for k, v := range params {
		merged[k] = v
	}
	merged["op"] = op

	raw, err := g.client.Call(ctx, "execute", merged)
	if err != nil && g.syncQueue != nil {
		entryID := fmt.Sprintf("sync-%s-%s-%s", g.adapterID, op, issueKey(params))
		entry, qerr := g.syncQueue.Enqueue(syncqueue.Entry{
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
