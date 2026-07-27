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

// sharedPromptPath holds the guidance common to every role (identity,
// communication rules, fact-versus-inference, disagreement handling). It is
// stored once and prepended to each role prompt at serve time, so the shared
// text lives in exactly one source file instead of being duplicated across the
// four role templates.
const sharedPromptPath = "shared/communication.md"

// registerPrompts adds the four role prompts (§28.4). Each served prompt is the
// shared communication guidance followed by that role's own template, composed
// here so the shared half is not duplicated in source.
func registerPrompts(server *mcp.Server) error {
	sharedBytes, err := prompts.FS.ReadFile(sharedPromptPath)
	if err != nil {
		return fmt.Errorf("mcpserver: read embedded prompt %s: %w", sharedPromptPath, err)
	}
	shared := string(sharedBytes)

	for name, path := range rolePrompts {
		content, err := prompts.FS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("mcpserver: read embedded prompt %s: %w", path, err)
		}
		text := shared + "\n\n---\n\n" + string(content)

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
