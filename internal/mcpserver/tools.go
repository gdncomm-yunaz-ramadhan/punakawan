package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/capability"
)

// addTool registers one MCP tool and records the same name in the capability
// registry used by workflow validation.
func addTool[In, Out any](server *mcp.Server, reg *toolIndex, tool *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(server, tool, h)
	if reg != nil {
		reg.Add(capability.Descriptor{Name: tool.Name, Source: capability.SourceMCP})
	}
}
