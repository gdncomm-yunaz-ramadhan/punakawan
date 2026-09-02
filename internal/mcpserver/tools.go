package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/capability"
)

// addTool registers one MCP tool and records the same name in the capability
// registry used by workflow validation. When reg carries a live tool-policy
// enforcement configuration (reg.agents non-nil), every call is checked
// against the calling session's bound role before h runs - see
// toolindex.go's checkToolPolicy. A reg with no agents configured (nil, or
// zero-value) enforces nothing, so every call path that predates this stays
// unchanged.
func addTool[In, Out any](server *mcp.Server, reg *toolIndex, tool *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	wrapped := h
	if reg != nil {
		wrapped = func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
			var session *mcp.ServerSession
			if req != nil {
				session = req.Session
			}
			if err := reg.checkToolPolicy(session, tool.Name); err != nil {
				var zero Out
				return nil, zero, err
			}
			return h(ctx, req, in)
		}
	}
	mcp.AddTool(server, tool, wrapped)
	if reg != nil {
		mutates := tool.Annotations == nil || !tool.Annotations.ReadOnlyHint
		reg.Add(capability.Descriptor{Name: tool.Name, Source: capability.SourceMCP, Mutates: mutates})
	}
}
