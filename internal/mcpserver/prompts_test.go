package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestRoleConfigPromptBlockRendersProjectPreferences proves the prompt block
// registerPrompts appends to a served role prompt reflects a project's saved
// style and free-text instructions, and never mentions permission or
// approval - roles.yaml only ever shapes prompt wording, it never authorizes
// a tool or gates a workflow stage.
func TestRoleConfigPromptBlockRendersProjectPreferences(t *testing.T) {
	a := newTestApp(t)

	cfg, err := roleconfig.Load(a.Workspace.Root)
	if err != nil {
		t.Fatalf("roleconfig.Load: %v", err)
	}
	strict := protocol.RolePreferenceStyleStrict
	instructions := "Prefer reversible migrations."
	if err := roleconfig.Update(cfg, roleconfig.Semar, roleconfig.Patch{
		Style:        &strict,
		Instructions: &instructions,
	}, cfg.Revision); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := roleconfig.Save(a.Workspace.Root, cfg, roleconfig.SaveOptions{Action: "test"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	block := roleConfigPromptBlock(a, roleconfig.Semar)
	if !strings.Contains(block, "Verify every required input") {
		t.Errorf("prompt block missing strict style guidance:\n%s", block)
	}
	if !strings.Contains(block, instructions) {
		t.Errorf("prompt block missing free-text instructions:\n%s", block)
	}
	if strings.Contains(block, "permission") || strings.Contains(block, "approval") {
		t.Errorf("prompt block must never mention permission or approval:\n%s", block)
	}
}

// TestRoleConfigPromptBlockFailsSoft proves a missing resolver never blocks
// prompt serving: it degrades to an empty block instead of erroring.
func TestRoleConfigPromptBlockFailsSoft(t *testing.T) {
	if got := roleConfigPromptBlock(nil, roleconfig.Semar); got != "" {
		t.Errorf("roleConfigPromptBlock(nil app) = %q, want empty", got)
	}

	a := newTestApp(t)
	a.RoleConfig = nil
	if got := roleConfigPromptBlock(a, roleconfig.Semar); got != "" {
		t.Errorf("roleConfigPromptBlock(nil RoleConfig) = %q, want empty", got)
	}
}

// TestServedPromptIncludesPromptPreferencesBlock proves the prompt a
// connected MCP client actually fetches (not just the internal helper) is
// the one carrying a project's live style/instructions choice.
func TestServedPromptIncludesPromptPreferencesBlock(t *testing.T) {
	a := newTestApp(t)

	cfg, err := roleconfig.Load(a.Workspace.Root)
	if err != nil {
		t.Fatalf("roleconfig.Load: %v", err)
	}
	creative := protocol.RolePreferenceStyleCreative
	instructions := "Always propose at least two alternatives."
	if err := roleconfig.Update(cfg, roleconfig.Petruk, roleconfig.Patch{
		Style:        &creative,
		Instructions: &instructions,
	}, cfg.Revision); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := roleconfig.Save(a.Workspace.Root, cfg, roleconfig.SaveOptions{Action: "test"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cs := connect(t, a)
	res, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "petruk"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("expected exactly one message, got %d", len(res.Messages))
	}
	text, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Messages[0].Content)
	}
	if !strings.Contains(text.Text, "Explore multiple viable approaches") {
		t.Errorf("served prompt missing creative style guidance:\n%s", text.Text)
	}
	if !strings.Contains(text.Text, instructions) {
		t.Errorf("served prompt missing free-text instructions:\n%s", text.Text)
	}
	if strings.Contains(text.Text, "permission") || strings.Contains(text.Text, "approval") {
		t.Errorf("served prompt must never mention permission or approval:\n%s", text.Text)
	}
}
