package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/approvals"
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
}

// NewGate constructs a Gate for an already-started adapter client and its
// manifest (as returned by the adapter's "initialize" call).
func NewGate(adapterID string, manifest protocol.AdapterManifest, client caller, store *approvals.Store) *Gate {
	return &Gate{adapterID: adapterID, manifest: manifest, client: client, approvals: store}
}

// approvalID is scoped only to runID, not the adapter or operation: one human
// approval covers every approval-required adapter write a run performs. A
// different run still needs its own approval; the boundary is "this run may
// write through configured adapters", not "write anything forever".
func approvalID(runID string) string {
	return fmt.Sprintf("approval-adapter-run-%s", runID)
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

	id := approvalID(runID)

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
	return g.approvals.Resolve(approvalID(runID), protocol.ApprovalRecordStatusApproved, approvedBy)
}

// Deny marks a pending adapter-write request as denied.
func (g *Gate) Deny(runID, approvedBy string) error {
	return g.approvals.Resolve(approvalID(runID), protocol.ApprovalRecordStatusDenied, approvedBy)
}

// Call invokes op via the adapter's "execute" method, first checking that
// an approved request exists if the manifest declares op approval-required.
// params is merged with {"op": op} before being sent, matching the
// prototype adapter's execute(params) convention of dispatching on a top-
// level "op" field (see packages/adapter-sdk/src/prototypeAdapter.ts).
func (g *Gate) Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error) {
	if g.requiresApproval(op) {
		current, err := g.approvals.Current()
		if err != nil {
			return nil, err
		}
		rec, ok := current[approvalID(runID)]
		if !ok || rec.Status != protocol.ApprovalRecordStatusApproved {
			id := approvalID(runID)
			return nil, fmt.Errorf("adapters: adapter %q is not approved for run %q (requested op %q, approval id %q); approve with `punakawan approvals approve %s --by <your-name>` and retry", g.adapterID, runID, op, id, id)
		}
	}

	merged := make(map[string]any, len(params)+1)
	for k, v := range params {
		merged[k] = v
	}
	merged["op"] = op

	return g.client.Call(ctx, "execute", merged)
}
