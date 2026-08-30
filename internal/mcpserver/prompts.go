package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/internal/roleconfig"
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
// shared communication guidance and that role's own template - both static,
// composed once here so the shared half is not duplicated in source - followed
// by a per-request role prompt-preferences block (roleconfig.PromptBlock)
// rendered fresh on every GetPrompt call from a's live project preferences and
// accepted learning proposals. That block is what actually varies per
// invocation: it is how a project's style/instructions choice and an accepted
// learning proposal reach a live Petruk/Bagong/Gareng/Semar role, since this
// GetPrompt handler - not roleconfig.PromptBlock's own unit tests - is the
// real prompt a connected MCP client fetches before reasoning as a role.
func registerPrompts(server *mcp.Server, a *app.App) error {
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
		role := roleconfig.Role(name)

		server.AddPrompt(&mcp.Prompt{
			Name:        name,
			Description: roleDescriptions[name],
		}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			full := text
			if block := roleConfigPromptBlock(a, role); block != "" {
				full = full + "\n\n---\n\n" + block
			}
			return &mcp.GetPromptResult{
				Description: roleDescriptions[name],
				Messages: []*mcp.PromptMessage{
					{Role: "user", Content: &mcp.TextContent{Text: full}},
				},
			}, nil
		})
	}
	return nil
}

// roleConfigPromptBlock renders role's live prompt preferences and currently
// accepted learning proposals via roleconfig.PromptBlock, for appending to a
// served role prompt. It fails soft (returns "") rather than blocking a role
// invocation: a nil resolver (no roles.yaml wiring, e.g. minimal test apps), a
// preferences read failure, or a learning-store read failure all just mean the
// dynamic block is omitted.
func roleConfigPromptBlock(a *app.App, role roleconfig.Role) string {
	if a == nil || a.RoleConfig == nil {
		return ""
	}
	pref, err := a.RoleConfig.Get("", role)
	if err != nil {
		return ""
	}
	var proposals []learning.Proposal
	if store, err := a.OpenLearning(); err == nil {
		proposals, _ = store.List()
	}
	return roleconfig.PromptBlock(role, pref, proposals)
}
