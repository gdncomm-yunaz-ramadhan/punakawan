package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// invokeAdapterOperation is the single MCP-facing path for adapter calls.
// Reads pass through immediately. For approval-required writes it first asks
// the connected MCP client to elicit a human decision for the whole run, then
// falls back to the approvals CLI only when that client does not advertise
// form elicitation.
func invokeAdapterOperation(
	ctx context.Context,
	req *mcp.CallToolRequest,
	gate *adapters.Gate,
	runID string,
	op string,
	params map[string]any,
	requestedBy protocol.ApprovalRecordRequestedBy,
) (json.RawMessage, error) {
	if err := ensureAdapterApproval(ctx, req, gate, runID, op, requestedBy); err != nil {
		return nil, err
	}
	return gate.Call(ctx, runID, op, params)
}

func ensureAdapterApproval(
	ctx context.Context,
	req *mcp.CallToolRequest,
	gate *adapters.Gate,
	runID string,
	op string,
	requestedBy protocol.ApprovalRecordRequestedBy,
) error {
	if !gate.RequiresApproval(op) {
		return nil
	}

	rec, err := gate.RequestApproval(runID, op, requestedBy)
	if err != nil {
		return fmt.Errorf("request adapter approval: %w", err)
	}
	switch rec.Status {
	case protocol.ApprovalRecordStatusApproved:
		return nil
	case protocol.ApprovalRecordStatusDenied:
		return fmt.Errorf("adapter write approval %q was denied for run %q", rec.Id, runID)
	}

	if !supportsFormElicitation(req) {
		return fmt.Errorf("adapter write approval %q is pending for run %q; the connected MCP client does not support form elicitation, so approve with `punakawan approvals approve %s --by <your-name>` and retry", rec.Id, runID, rec.Id)
	}

	result, err := req.Session.Elicit(ctx, &mcp.ElicitParams{
		Mode: "form",
		Message: fmt.Sprintf(
			"Approve Punakawan to perform approval-required adapter writes for run %q? This one approval covers writes through all configured adapters during this run (first requested target: %q, operation: %q).",
			runID, recTarget(rec), op,
		),
	})
	if err != nil {
		// The capability was advertised, so this is a real elicitation failure,
		// not a reason to silently switch channels and surprise the user.
		return fmt.Errorf("elicit adapter write approval %q: %w", rec.Id, err)
	}

	switch result.Action {
	case "accept":
		if err := gate.Approve(runID, elicitationApprover(req)); err != nil {
			return fmt.Errorf("record elicited adapter approval %q: %w", rec.Id, err)
		}
		return nil
	case "decline":
		if err := gate.Deny(runID, elicitationApprover(req)); err != nil {
			return fmt.Errorf("record declined adapter approval %q: %w", rec.Id, err)
		}
		return fmt.Errorf("adapter write approval %q was declined by the user", rec.Id)
	case "cancel":
		return fmt.Errorf("adapter write approval %q was cancelled; no write was performed", rec.Id)
	default:
		return fmt.Errorf("adapter write approval %q returned unknown elicitation action %q", rec.Id, result.Action)
	}
}

func supportsFormElicitation(req *mcp.CallToolRequest) bool {
	if req == nil || req.Session == nil {
		return false
	}
	init := req.Session.InitializeParams()
	if init == nil || init.Capabilities == nil || init.Capabilities.Elicitation == nil {
		return false
	}
	caps := init.Capabilities.Elicitation
	// The MCP SDK treats an elicitation capability with neither subtype set
	// as form support for backward compatibility.
	return caps.Form != nil || caps.URL == nil
}

func elicitationApprover(req *mcp.CallToolRequest) string {
	const channel = "user-via-mcp-elicitation"
	if req == nil || req.Session == nil {
		return channel
	}
	init := req.Session.InitializeParams()
	if init == nil || init.ClientInfo == nil || init.ClientInfo.Name == "" {
		return channel
	}
	return fmt.Sprintf("%s:%s", channel, init.ClientInfo.Name)
}

func recTarget(rec protocol.ApprovalRecord) string {
	if rec.Target == nil || *rec.Target == "" {
		return "unknown"
	}
	return *rec.Target
}
