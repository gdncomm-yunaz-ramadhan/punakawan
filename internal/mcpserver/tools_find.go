package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// findToolMaxResults caps one find_tool call's matches, so a broad query
// can't flood tools/list back up toward the full worker-side surface in one
// shot - a caller narrows the query instead.
const findToolMaxResults = 15

// FindToolInput is find_tool's input.
type FindToolInput struct {
	Query string `json:"query" jsonschema:"keywords to match against hidden tool names/descriptions, or 'select:name1,name2' for exact names"`
}

// FindToolOutput is find_tool's output.
type FindToolOutput struct {
	Matches []toolMatch `json:"matches"`
}

// registerToolFinder adds find_tool: the one always-visible tool that
// reveals the rest of the worker-side surface on demand, so the default
// facade stays small without making any capability unreachable.
func registerToolFinder(server *mcp.Server, idx *toolIndex) {
	addTool(server, idx, &mcp.Tool{
		Name:        "find_tool",
		Description: "Search the full tool surface beyond the default facade (Jira, knowledge, git/PR, role review, adapters, ...) by keyword, or 'select:name1,name2' for exact names. Each match becomes callable immediately - no separate enable step. Narrow the query; results are capped.",
	}, findToolHandler(idx))
}

func findToolHandler(idx *toolIndex) func(context.Context, *mcp.CallToolRequest, FindToolInput) (*mcp.CallToolResult, FindToolOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in FindToolInput) (*mcp.CallToolResult, FindToolOutput, error) {
		return nil, FindToolOutput{Matches: idx.find(in.Query, findToolMaxResults)}, nil
	}
}
