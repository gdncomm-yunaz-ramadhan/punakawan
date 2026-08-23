package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ListPendingApprovalsInput optionally narrows the result to a single run.
type ListPendingApprovalsInput struct {
	RunId string `json:"run_id,omitempty" jsonschema:"optional run id to filter to a single run's pending approvals; omit to list all pending approvals in this project"`
}

// PendingApprovalItem is one pending approval, flattened for the agent.
type PendingApprovalItem struct {
	ApprovalId  string `json:"approval_id"`
	RunId       string `json:"run_id"`
	Operation   string `json:"operation"`
	Target      string `json:"target,omitempty"`
	Preview     string `json:"preview,omitempty"`
	RequestedBy string `json:"requested_by"`
	CreatedAt   string `json:"created_at"`
}

type ListPendingApprovalsOutput struct {
	Pending    []PendingApprovalItem `json:"pending"`
	Count      int                   `json:"count"`
	NextAction string                `json:"next_action"`
}

// listPendingApprovalsHandler backs the list_pending_approvals tool: a
// read-only re-check of the durable approval queue. Approvals are persisted
// per run_id (approvals.jsonl), so this returns them even after the session
// that requested them ended - letting a looping or freshly-resumed agent see
// what is still pending and proceed without blindly retrying a write.
func listPendingApprovalsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListPendingApprovalsInput) (*mcp.CallToolResult, ListPendingApprovalsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListPendingApprovalsInput) (*mcp.CallToolResult, ListPendingApprovalsOutput, error) {
		store, err := a.OpenApprovals()
		if err != nil {
			return nil, ListPendingApprovalsOutput{}, fmt.Errorf("mcpserver: open approvals: %w", err)
		}
		out, err := listPendingApprovals(store, in)
		return nil, out, err
	}
}

func listPendingApprovals(store *approvals.Store, in ListPendingApprovalsInput) (ListPendingApprovalsOutput, error) {
	pending, err := store.Pending()
	if err != nil {
		return ListPendingApprovalsOutput{}, fmt.Errorf("mcpserver: list pending approvals: %w", err)
	}
	items := make([]PendingApprovalItem, 0, len(pending))
	for _, rec := range pending {
		if in.RunId != "" && rec.RunId != in.RunId {
			continue
		}
		items = append(items, toPendingItem(rec))
	}
	out := ListPendingApprovalsOutput{Pending: items, Count: len(items)}
	if len(items) == 0 {
		out.NextAction = "No pending approvals. If a prior write was already approved for this run, retry it; the approval covers all adapter writes for the same run."
	} else {
		out.NextAction = "For each pending approval, show the user the Approve/Deny choice (never choose for them) and call respond_to_adapter_approval with the approval_id and confirmed_by, then retry the original write. Safe to re-check this list on a loop or in a new session."
	}
	return out, nil
}

func toPendingItem(rec protocol.ApprovalRecord) PendingApprovalItem {
	item := PendingApprovalItem{
		ApprovalId:  rec.Id,
		RunId:       rec.RunId,
		Operation:   string(rec.Operation),
		RequestedBy: string(rec.RequestedBy),
		CreatedAt:   rec.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if rec.Target != nil {
		item.Target = *rec.Target
	}
	if rec.Preview != nil {
		item.Preview = *rec.Preview
	}
	return item
}
