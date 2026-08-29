package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
)

type ListAdapterOperationsInput struct {
	AdapterID string `json:"adapter_id,omitempty"`
}

type AdapterOperation struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	SideEffect  bool           `json:"side_effect"`
	Approval    string         `json:"approval,omitempty"`
}

type AdapterOperations struct {
	ID         string             `json:"id"`
	Name       string             `json:"name"`
	Provides   []string           `json:"provides"`
	Operations []AdapterOperation `json:"operations"`
}

type ListAdapterOperationsOutput struct {
	Adapters []AdapterOperations `json:"adapters"`
}

func listAdapterOperationsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListAdapterOperationsInput) (*mcp.CallToolResult, ListAdapterOperationsOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListAdapterOperationsInput) (*mcp.CallToolResult, ListAdapterOperationsOutput, error) {
		ids := adapterIDs(a.AdapterRegistry, in.AdapterID)
		out := ListAdapterOperationsOutput{Adapters: make([]AdapterOperations, 0, len(ids))}
		for _, id := range ids {
			gate, err := a.AdapterRegistry.Gate(ctx, id)
			if err != nil {
				return nil, ListAdapterOperationsOutput{}, fmt.Errorf("mcpserver: open adapter %q: %w", id, err)
			}
			manifest := gate.Manifest()
			operations := make([]AdapterOperation, 0, len(manifest.Operations))
			for name, metadata := range manifest.Operations {
				approval := ""
				if metadata.Approval != nil {
					approval = string(*metadata.Approval)
				}
				operations = append(operations, AdapterOperation{
					Name:        name,
					Description: metadata.Description,
					InputSchema: map[string]any(metadata.InputSchema),
					SideEffect:  metadata.SideEffect,
					Approval:    approval,
				})
			}
			sort.Slice(operations, func(i, j int) bool { return operations[i].Name < operations[j].Name })
			out.Adapters = append(out.Adapters, AdapterOperations{ID: id, Name: manifest.Name, Provides: manifest.Provides, Operations: operations})
		}
		return nil, out, nil
	}
}

type CallAdapterOperationInput struct {
	AdapterID  string         `json:"adapter_id"`
	Operation  string         `json:"operation"`
	RunID      string         `json:"run_id"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type CallAdapterOperationOutput struct {
	Result json.RawMessage `json:"result"`
}

func callAdapterOperationHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CallAdapterOperationInput) (*mcp.CallToolResult, CallAdapterOperationOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CallAdapterOperationInput) (*mcp.CallToolResult, CallAdapterOperationOutput, error) {
		if in.AdapterID == "" || in.Operation == "" || in.RunID == "" {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: call adapter operation requires adapter_id, operation, and run_id")
		}
		gate, err := a.AdapterRegistry.Gate(ctx, in.AdapterID)
		if err != nil {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: open adapter %q: %w", in.AdapterID, err)
		}
		if _, ok := gate.Manifest().Operations[in.Operation]; !ok {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: adapter operation %q is not declared by %q", in.Operation, in.AdapterID)
		}
		result, err := gate.Call(ctx, in.RunID, in.Operation, in.Parameters)
		if err != nil {
			return nil, CallAdapterOperationOutput{}, fmt.Errorf("mcpserver: call adapter operation %q: %w", in.Operation, err)
		}
		return nil, CallAdapterOperationOutput{Result: result}, nil
	}
}

func adapterIDs(registry *adapters.Registry, requested string) []string {
	if requested != "" {
		return []string{requested}
	}
	specs := registry.Specs()
	ids := make([]string, 0, len(specs))
	for id := range specs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
