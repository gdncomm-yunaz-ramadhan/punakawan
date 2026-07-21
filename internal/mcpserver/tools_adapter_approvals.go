package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// RespondToAdapterApprovalInput records a choice the connected agent has
// already received explicitly from the human user. It is deliberately a
// separate follow-up call: the write operation cannot approve itself.
type RespondToAdapterApprovalInput struct {
	ApprovalId  string `json:"approval_id" jsonschema:"pending id returned by a write call; must start with approval-adapter-run-"`
	Decision    string `json:"decision" jsonschema:"one of approve|deny; copy the user's explicit choice and never infer it"`
	ConfirmedBy string `json:"confirmed_by" jsonschema:"human name or identifier supplied by or representing the user who explicitly chose"`
}

type RespondToAdapterApprovalOutput struct {
	ApprovalId string                        `json:"approval_id"`
	RunId      string                        `json:"run_id"`
	Status     protocol.ApprovalRecordStatus `json:"status"`
	NextAction string                        `json:"next_action"`
}

func respondToAdapterApprovalHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RespondToAdapterApprovalInput) (*mcp.CallToolResult, RespondToAdapterApprovalOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RespondToAdapterApprovalInput) (*mcp.CallToolResult, RespondToAdapterApprovalOutput, error) {
		out, err := respondToAdapterApproval(a.Approvals, in)
		return nil, out, err
	}
}

func respondToAdapterApproval(store *approvals.Store, in RespondToAdapterApprovalInput) (RespondToAdapterApprovalOutput, error) {
	if !strings.HasPrefix(in.ApprovalId, "approval-adapter-run-") {
		return RespondToAdapterApprovalOutput{}, fmt.Errorf("mcpserver: respond_to_adapter_approval only resolves adapter-run approvals")
	}
	if strings.TrimSpace(in.ConfirmedBy) == "" {
		return RespondToAdapterApprovalOutput{}, fmt.Errorf("mcpserver: respond_to_adapter_approval requires confirmed_by from the explicit user choice")
	}
	current, err := store.Current()
	if err != nil {
		return RespondToAdapterApprovalOutput{}, fmt.Errorf("mcpserver: read adapter approval: %w", err)
	}
	rec, ok := current[in.ApprovalId]
	if !ok {
		return RespondToAdapterApprovalOutput{}, fmt.Errorf("mcpserver: adapter approval %q not found", in.ApprovalId)
	}

	var status protocol.ApprovalRecordStatus
	var next string
	switch strings.ToLower(strings.TrimSpace(in.Decision)) {
	case "approve":
		status = protocol.ApprovalRecordStatusApproved
		next = "Retry the original adapter write; this approval covers all adapter writes for the same run."
	case "deny":
		status = protocol.ApprovalRecordStatusDenied
		next = "Do not retry or perform adapter writes for this run."
	default:
		return RespondToAdapterApprovalOutput{}, fmt.Errorf("mcpserver: decision must be exactly approve or deny")
	}
	if err := store.Resolve(in.ApprovalId, status, strings.TrimSpace(in.ConfirmedBy)); err != nil {
		return RespondToAdapterApprovalOutput{}, fmt.Errorf("mcpserver: resolve adapter approval: %w", err)
	}
	return RespondToAdapterApprovalOutput{ApprovalId: in.ApprovalId, RunId: rec.RunId, Status: status, NextAction: next}, nil
}
