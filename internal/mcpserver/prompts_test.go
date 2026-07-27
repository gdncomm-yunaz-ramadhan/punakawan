package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/prompts"
)

// servedPrompt fetches a role prompt as the connected client sees it (shared
// guidance + role template, composed by registerPrompts).
func servedPrompt(t *testing.T, cs *mcp.ClientSession, role string) string {
	t.Helper()
	res, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: role})
	if err != nil {
		t.Fatalf("GetPrompt %q: %v", role, err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("%s: Messages = %d, want 1", role, len(res.Messages))
	}
	tc, ok := res.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("%s: Content = %T, want *mcp.TextContent", role, res.Messages[0].Content)
	}
	return tc.Text
}

// sharedOnlyPhrases live in shared/communication.md and must NOT be copied into
// any per-role template — they are the dedup guard.
var sharedOnlyPhrases = []string{
	"grounded truth over confident performance",
	"## Communication rules",
	"trusted data and provenance boundary",
	"`observed`, `inferred`, `assumed`, `verified`",
}

// TestRolePromptsComposeSharedGuidanceOnce verifies every served role prompt
// carries the shared guidance exactly once, so the connected client gets the
// full context without the shared text being duplicated within one prompt.
func TestRolePromptsComposeSharedGuidanceOnce(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	for _, role := range []string{"semar", "gareng", "petruk", "bagong"} {
		text := servedPrompt(t, cs, role)
		for _, phrase := range sharedOnlyPhrases {
			if n := strings.Count(text, phrase); n != 1 {
				t.Errorf("%s served prompt contains %q %d times, want exactly 1", role, phrase, n)
			}
		}
		if !strings.Contains(text, "grounded truth over confident performance") {
			t.Errorf("%s served prompt missing the shared product principle", role)
		}
	}
}

// TestSharedGuidanceNotDuplicatedInRoleTemplates asserts dedup at the source:
// the raw per-role templates must not restate the shared-only guidance.
func TestSharedGuidanceNotDuplicatedInRoleTemplates(t *testing.T) {
	for _, path := range []string{"semar/prompt.md", "gareng/prompt.md", "petruk/prompt.md", "bagong/prompt.md"} {
		b, err := prompts.FS.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(b)
		for _, phrase := range sharedOnlyPhrases {
			if strings.Contains(body, phrase) {
				t.Errorf("%s duplicates shared-only guidance %q; it belongs only in shared/communication.md", path, phrase)
			}
		}
	}
}

// TestRolePromptsHaveDistinctOutputContracts verifies each role advertises its
// own concise preferred-output shape, tone, and short principle — the four are
// not interchangeable.
func TestRolePromptsHaveDistinctOutputContracts(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	cases := map[string]struct {
		principle string
		shape     []string
	}{
		"semar":  {"Ground the work.", []string{"Purpose", "Decision", "Open issue", "Next step"}},
		"gareng": {"Notice what others miss.", []string{"Assessment", "Blocking risks", "Important cautions", "Impact"}},
		"petruk": {"Make the idea useful.", []string{"Recommended solution", "Implementation steps", "Trade-offs", "Verification"}},
		"bagong": {"Say what is true.", []string{"Verdict", "Blocking findings", "Verified", "Unverified"}},
	}

	texts := map[string]string{}
	for role, want := range cases {
		text := servedPrompt(t, cs, role)
		texts[role] = text
		if !strings.Contains(text, want.principle) {
			t.Errorf("%s prompt missing its short principle %q", role, want.principle)
		}
		for _, line := range want.shape {
			if !strings.Contains(text, line) {
				t.Errorf("%s prompt missing preferred-output line %q", role, line)
			}
		}
	}

	// No role should carry another role's short principle.
	for role := range cases {
		for other, ow := range cases {
			if role == other {
				continue
			}
			if strings.Contains(texts[role], ow.principle) {
				t.Errorf("%s prompt should not contain %s's principle %q", role, other, ow.principle)
			}
		}
	}
}
