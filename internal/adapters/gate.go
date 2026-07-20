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

func approvalID(adapterID, op, runID string) string {
	return fmt.Sprintf("approval-adapter-%s-%s-%s", adapterID, op, runID)
}

// requiresApproval reports whether op is declared approval-required in the
// adapter's manifest. Operations the manifest doesn't mention at all, or
// declares without an approval requirement, are not gated.
func (g *Gate) requiresApproval(op string) bool {
	entry, ok := g.manifest.Operations[op]
	return ok && entry.Approval != nil && *entry.Approval == protocol.AdapterManifestOperationsValueApprovalRequired
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

// RequestApproval creates a pending approval record for invoking op, or
// returns the existing record if one was already requested (idempotent).
// It is a no-op error to call this for an operation the manifest does not
// require approval for; callers should check requiresApproval-equivalent
// behavior implicitly by simply calling Call, which only enforces the gate
// when the manifest asks for it.
func (g *Gate) RequestApproval(runID, op string, requestedBy protocol.ApprovalRecordRequestedBy) (protocol.ApprovalRecord, error) {
	id := approvalID(g.adapterID, op, runID)

	current, err := g.approvals.Current()
	if err != nil {
		return protocol.ApprovalRecord{}, err
	}
	if rec, ok := current[id]; ok {
		return rec, nil
	}

	target := fmt.Sprintf("%s:%s", g.adapterID, op)
	reason := fmt.Sprintf("invoke adapter operation %q on %q", op, g.adapterID)
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

// Approve marks a pending operation request as approved.
func (g *Gate) Approve(runID, op, approvedBy string) error {
	return g.resolve(runID, op, protocol.ApprovalRecordStatusApproved, approvedBy)
}

// Deny marks a pending operation request as denied.
func (g *Gate) Deny(runID, op, approvedBy string) error {
	return g.resolve(runID, op, protocol.ApprovalRecordStatusDenied, approvedBy)
}

func (g *Gate) resolve(runID, op string, status protocol.ApprovalRecordStatus, approvedBy string) error {
	id := approvalID(g.adapterID, op, runID)
	current, err := g.approvals.Current()
	if err != nil {
		return err
	}
	rec, ok := current[id]
	if !ok {
		return fmt.Errorf("adapters: no approval request %q; call RequestApproval first", id)
	}

	now := time.Now().UTC()
	rec.Status = status
	rec.ApprovedBy = &approvedBy
	rec.ResolvedAt = &now
	return g.approvals.Append(rec)
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
		rec, ok := current[approvalID(g.adapterID, op, runID)]
		if !ok || rec.Status != protocol.ApprovalRecordStatusApproved {
			return nil, fmt.Errorf("adapters: operation %q on %q is not approved for run %q", op, g.adapterID, runID)
		}
	}

	merged := make(map[string]any, len(params)+1)
	for k, v := range params {
		merged[k] = v
	}
	merged["op"] = op

	return g.client.Call(ctx, "execute", merged)
}
