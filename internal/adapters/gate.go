package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/syncqueue"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// sensitiveKeyRe matches param keys whose values must never appear verbatim in
// an approval preview - tokens, passwords, and the like. The whole point of
// the preview is to let a human see WHAT is being written, so it renders the
// payload; that must not become a channel for leaking a secret that happened
// to ride along in the same params map.
var sensitiveKeyRe = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[_-]?key|apikey|authorization|auth|cookie|credential|private[_-]?key)`)

// approvalPreviewMaxLen bounds a rendered preview so a large adapter payload
// (an attachment body, a long Confluence page) can't bloat the append-only
// approvals.jsonl or the panel that renders it.
const approvalPreviewMaxLen = 2000

// BuildApprovalPreview renders a bounded, secret-redacted view of an adapter
// operation and its params, so a human resolving the approval - on the panel
// or through MCP elicitation - can see the actual content they are
// authorizing, not just the operation category. Values under sensitive-looking
// keys (see sensitiveKeyRe) are masked, and the whole preview is capped at
// approvalPreviewMaxLen. Returns "" when there is nothing meaningful to show,
// so RequestApproval leaves Preview unset rather than storing an empty shell.
func BuildApprovalPreview(op string, params map[string]any) string {
	redacted := redactParams(params)
	var b strings.Builder
	fmt.Fprintf(&b, "operation: %s\n", op)
	if len(redacted) == 0 {
		b.WriteString("params: (none)")
	} else if enc, err := json.MarshalIndent(redacted, "", "  "); err != nil {
		fmt.Fprintf(&b, "params: (unrenderable: %v)", err)
	} else {
		b.WriteString("params:\n")
		b.Write(enc)
	}
	out := b.String()
	if len(out) > approvalPreviewMaxLen {
		out = out[:approvalPreviewMaxLen] + "\n… (truncated)"
	}
	return out
}

// redactParams deep-copies params, masking any value whose key looks sensitive
// (recursing into nested maps). Non-map values are copied by reference, which
// is fine because the result is only ever marshaled to JSON, never mutated.
func redactParams(params map[string]any) map[string]any {
	if len(params) == 0 {
		return nil
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		if sensitiveKeyRe.MatchString(k) {
			out[k] = "***redacted***"
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			out[k] = redactParams(nested)
			continue
		}
		out[k] = v
	}
	return out
}

// caller is the subset of *Client's behavior Gate depends on, so tests can
// substitute a fake instead of spawning a real adapter subprocess.
type caller interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// Gate wraps an adapter Client. It no longer enforces the manifest-declared
// approval requirement before invoking an operation - Call always proceeds
// straight to the adapter. RequestApproval/Approve/Deny and the approvals
// store remain as an inert audit-trail API (mirroring gitops.WorktreeManager's
// RequestApproval/Approve/Deny pattern) that nothing in this codebase calls
// on the normal write path anymore.
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
	// syncQueue lazily resolves the store that records a write which reaches
	// the adapter (i.e. passed the approval check) but fails, for later retry
	// (punokawan-nbz). nil by default, so a Gate built without calling
	// SetSyncQueue (every existing test does) keeps the original behavior of
	// simply returning the error. It is a provider rather than a resolved
	// store so a Gate never forces the shared storage kernel open until a
	// write actually fails.
	syncQueue func() (*syncqueue.Queue, error)
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

// SetSyncQueue configures provider to record any write this Gate makes that
// fails after passing its approval check, so it can be found and retried
// later (punokawan-nbz) instead of the failure only ever existing as the
// error returned from that one Call. provider is resolved lazily, only when
// a write actually fails.
func (g *Gate) SetSyncQueue(provider func() (*syncqueue.Queue, error)) {
	g.syncQueue = provider
}

// Manifest returns the authoritative manifest obtained from this adapter when
// the Gate was initialized. Callers must treat the returned value as read-only.
func (g *Gate) Manifest() protocol.AdapterManifest {
	return g.manifest
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

// requiresApproval always reports false: approval gating has been removed,
// so no adapter operation is ever blocked pending approval.
func (g *Gate) requiresApproval(op string) bool {
	return false
}

// RequiresApproval always reports false now that approval gating has been
// removed. Kept so existing callers (which branch on it before optionally
// requesting/granting approval) keep compiling and simply skip that branch.
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
// operation this run performs against this adapter, or returns the existing
// record if one was already requested (idempotent) - including when a
// different adapter or operation in the same run created it, since approval
// is scoped to the run, not the adapter or individual op (see approvalID).
// Nothing in this codebase's normal write path calls this anymore (approval
// no longer gates Call); it remains only as an explicit audit-trail API.
func (g *Gate) RequestApproval(runID, op string, requestedBy protocol.ApprovalRecordRequestedBy, preview ...string) (protocol.ApprovalRecord, error) {
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
	// The approval is scoped to the run, so the preview reflects the first
	// operation that triggered it - the same "first requested" framing the
	// reason string uses. This is what the human sees on the panel / in the
	// elicitation prompt: the actual content being authorized, not just the
	// operation category.
	if len(preview) > 0 && preview[0] != "" {
		p := preview[0]
		rec.Preview = &p
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
