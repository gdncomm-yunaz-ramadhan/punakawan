package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// invokeAdapterOperation is the single MCP-facing path for adapter calls.
// Reads pass through immediately. For approval-required writes it first asks
// the connected MCP client to elicit a human decision for the whole run, then
// falls back to a conversational Approve/Deny choice (or the approvals CLI)
// when that client cannot perform form elicitation.
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

	// Always attempt MCP form elicitation first. Some clients have historically
	// implemented the request before advertising the capability correctly; the
	// SDK itself returns an unsupported error without sending when it truly
	// cannot elicit.
	var result *mcp.ElicitResult
	if req != nil && req.Session != nil {
		result, err = req.Session.Elicit(ctx, &mcp.ElicitParams{
			Mode: "form",
			Message: fmt.Sprintf(
				"Punakawan requests permission to write for run %q. Choose Approve to allow all configured adapter writes in this run, or Deny to block them. First target: %q; operation: %q.",
				runID, recTarget(rec), op,
			),
		})
	} else {
		err = fmt.Errorf("client does not support elicitation")
	}
	if err != nil {
		if elicitationUnavailable(err) {
			return fmt.Errorf("adapter write approval %q is pending for run %q. ACTION REQUIRED: ask the user to choose one option, and do not choose for them: [Approve] allow all configured adapter writes for this run; [Deny] block adapter writes for this run. After the user explicitly chooses, call respond_to_adapter_approval with approval_id=%q, decision=approve|deny, and confirmed_by=<user>, then retry the original operation only if approved. CLI alternative: `punakawan approvals approve %s --by <your-name>` or `punakawan approvals deny %s --by <your-name>`", rec.Id, runID, rec.Id, rec.Id, rec.Id)
		}
		return fmt.Errorf("elicit adapter write approval %q: %w", rec.Id, err)
	}
	if result == nil {
		return fmt.Errorf("elicit adapter write approval %q: client returned no result", rec.Id)
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

func elicitationUnavailable(err error) bool {
	detail := strings.ToLower(err.Error())
	return strings.Contains(detail, "does not support elicitation") ||
		strings.Contains(detail, "does not support \"form\" elicitation") ||
		strings.Contains(detail, "method not found") ||
		strings.Contains(detail, "unsupported method")
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
