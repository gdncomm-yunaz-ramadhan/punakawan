package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
)

// CallAdapterOperationInput is call_adapter_operation's input.
type CallAdapterOperationInput struct {
	RunId       string         `json:"run_id"`
	AdapterId   string         `json:"adapter_id" jsonschema:"registered adapter id, e.g. atlassian, docling"`
	Op          string         `json:"op" jsonschema:"operation name declared in the adapter's manifest, e.g. atlassian.getJiraIssue"`
	Params      map[string]any `json:"params,omitempty" jsonschema:"operation-specific parameters, passed through to the adapter unchanged"`
	RequestedBy string         `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation, recorded if approval is required"`
	// Fields, when non-empty, projects the adapter result down to just these
	// keys before returning it, so a caller that only needs a few fields of a
	// large Jira/Confluence payload does not pay the full envelope in model
	// context. Dot notation selects one level of nesting (e.g. "fields.summary").
	// Omitting fields returns the adapter result unchanged (§3.2 passthrough).
	Fields []string `json:"fields,omitempty" jsonschema:"optional top-level (or dotted one-level) keys to keep from the result; omit for the full payload"`
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
		requestedBy, err := validateRequestedBy(in.RequestedBy)
		if err != nil {
			return nil, CallAdapterOperationOutput{}, err
		}

		gate, err := a.AdapterRegistry.Gate(ctx, in.AdapterId)
		if err != nil {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: call_adapter_operation: %w", err)
		}

		raw, err := invokeAdapterOperation(ctx, req, gate, in.RunId, in.Op, in.Params, requestedBy)
		if err != nil {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: call_adapter_operation: %w", err)
		}

		var result map[string]any
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: decode adapter result: %w", err)
		}

		if len(in.Fields) > 0 {
			result = projectResult(result, in.Fields)
		}

		return nil, CallAdapterOperationOutput{Result: result}, nil
	}
}

// projectResult keeps only the requested keys from result. A key may be a
// top-level name ("summary") or a single-level dotted path ("fields.summary"),
// in which case the nested value is preserved under its parent. Requested keys
// that do not exist are skipped. The original map is not mutated.
func projectResult(result map[string]any, fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		if f == "" {
			continue
		}
		parent, child, nested := strings.Cut(f, ".")
		if !nested {
			if v, ok := result[f]; ok {
				out[f] = v
			}
			continue
		}
		sub, ok := result[parent].(map[string]any)
		if !ok {
			continue
		}
		val, ok := sub[child]
		if !ok {
			continue
		}
		dst, ok := out[parent].(map[string]any)
		if !ok {
			dst = make(map[string]any)
			out[parent] = dst
		}
		dst[child] = val
	}
	return out
}
