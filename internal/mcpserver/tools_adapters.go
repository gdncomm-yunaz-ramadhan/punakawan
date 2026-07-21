package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CallAdapterOperationInput is call_adapter_operation's input.
type CallAdapterOperationInput struct {
	RunId       string         `json:"run_id"`
	AdapterId   string         `json:"adapter_id" jsonschema:"registered adapter id, e.g. atlassian, docling"`
	Op          string         `json:"op" jsonschema:"operation name declared in the adapter's manifest, e.g. atlassian.getJiraIssue"`
	Params      map[string]any `json:"params,omitempty" jsonschema:"operation-specific parameters, passed through to the adapter unchanged"`
	RequestedBy string         `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation, recorded if approval is required"`
}

// CallAdapterOperationOutput is call_adapter_operation's output: whatever
// JSON object the adapter's execute handler returned, passed through
// unchanged. Go core does not interpret adapter-operation-specific response
// shapes - that semantic knowledge belongs to the TypeScript adapter and its
// caller (§3.2's TS-owned normalization boundary).
type CallAdapterOperationOutput struct {
	Result map[string]any `json:"result,omitempty"`
}

// callAdapterOperationHandler invokes any adapter operation declared in that
// adapter's manifest (§5.3), starting the adapter process on first use
// (adapters.Registry.Gate) and enforcing its approval requirements
// (adapters.Gate.Call). Callers with a not-yet-approved, approval-required
// op are shown an inline human elicitation when the client supports it. The
// CLI remains the fallback for clients without elicitation support; approval
// is deliberately not exposed as another agent-callable MCP tool.
func callAdapterOperationHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CallAdapterOperationInput) (*mcp.CallToolResult, CallAdapterOperationOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CallAdapterOperationInput) (*mcp.CallToolResult, CallAdapterOperationOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, in.AdapterId)
		if err != nil {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: call_adapter_operation: %w", err)
		}

		raw, err := invokeAdapterOperation(ctx, req, gate, in.RunId, in.Op, in.Params, protocol.ApprovalRecordRequestedBy(in.RequestedBy))
		if err != nil {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: call_adapter_operation: %w", err)
		}

		var result map[string]any
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: decode adapter result: %w", err)
		}

		return nil, CallAdapterOperationOutput{Result: result}, nil
	}
}
