// tools_role.go implements role_list and role_get, the structured-discovery
// MCP surface over internal/agent's RoleSpec manifests. This is additive
// discovery only: the native MCP prompts (registerPrompts in prompts.go)
// remain the actual mechanism a client fetches to reason as a role: they
// serve the shared communication guidance plus live roleconfig
// preferences/learning proposals, none of which role_get re-derives here.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/agent"
	"github.com/ygrip/punakawan/prompts"
)

// RoleSummary is role_list's per-role shape: lean, discovery-only fields
// with no instructions/schema/tool-policy detail (fetch role_get for
// that).
type RoleSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

type RoleListOutput struct {
	Roles []RoleSummary `json:"roles"`
}

func roleListHandler(reg agent.AgentRegistry) func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, RoleListOutput, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, RoleListOutput, error) {
		specs := reg.List()
		out := make([]RoleSummary, 0, len(specs))
		for _, spec := range specs {
			out = append(out, RoleSummary{ID: spec.ID, Name: spec.Name, Description: spec.Description, Version: spec.Version})
		}
		return nil, RoleListOutput{Roles: out}, nil
	}
}

type RoleGetInput struct {
	ID string `json:"id"`
}

// RoleGetOutput is role_get's output: the full RoleSpec, except
// Instructions carries the role's actual instruction text (read via
// prompts.FS) rather than the manifest's bare path string.
type RoleGetOutput struct {
	Role agent.RoleSpec `json:"role"`
}

func roleGetHandler(reg agent.AgentRegistry) func(context.Context, *mcp.CallToolRequest, RoleGetInput) (*mcp.CallToolResult, RoleGetOutput, error) {
	return func(_ context.Context, _ *mcp.CallToolRequest, in RoleGetInput) (*mcp.CallToolResult, RoleGetOutput, error) {
		if in.ID == "" {
			return nil, RoleGetOutput{}, fmt.Errorf("mcpserver: role_get: id is required")
		}
		spec, err := reg.Get(in.ID)
		if err != nil {
			return nil, RoleGetOutput{}, fmt.Errorf("mcpserver: role_get: %w", err)
		}
		text, err := prompts.FS.ReadFile(spec.Instructions)
		if err != nil {
			return nil, RoleGetOutput{}, fmt.Errorf("mcpserver: role_get: read instructions %s: %w", spec.Instructions, err)
		}
		spec.Instructions = string(text)
		return nil, RoleGetOutput{Role: spec}, nil
	}
}
