package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/prompts"
)

// rolePrompts maps each MCP prompt name (§28.4) to its embedded template
// file.
var rolePrompts = map[string]string{
	"semar":  "semar/prompt.md",
	"gareng": "gareng/prompt.md",
	"petruk": "petruk/prompt.md",
	"bagong": "bagong/prompt.md",
}

var roleDescriptions = map[string]string{
	"semar":  "Interpret intent, build the context dossier, consolidate Gareng/Petruk findings, and produce clarification questions or the final plan (§8.1).",
	"gareng": "Review feasibility, risk, compatibility, and acceptance-criteria quality (§8.2).",
	"petruk": "Challenge the request for simpler alternatives and produce an implementation plan (§8.3).",
	"bagong": "Independently review completed work against raw evidence (§8.4).",
}

// registerPrompts adds the four role prompts (§28.4), each serving its
// embedded template file verbatim as a single user-role message.
func registerPrompts(server *mcp.Server) error {
	for name, path := range rolePrompts {
		content, err := prompts.FS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("mcpserver: read embedded prompt %s: %w", path, err)
		}
		text := string(content)

		server.AddPrompt(&mcp.Prompt{
			Name:        name,
			Description: roleDescriptions[name],
		}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{
				Description: roleDescriptions[name],
				Messages: []*mcp.PromptMessage{
					{Role: "user", Content: &mcp.TextContent{Text: text}},
				},
			}, nil
		})
	}
	return nil
}
