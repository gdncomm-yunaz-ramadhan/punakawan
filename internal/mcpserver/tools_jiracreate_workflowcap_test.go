package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// saveWorkflowDefWithCapability calls save_workflow_definition with a single
// step whose capability is cap, returning the raw result so a test can assert
// on either success or the error content.
func saveWorkflowDefWithCapability(t *testing.T, cs *mcp.ClientSession, id, cap string) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "save_workflow_definition",
		Arguments: map[string]any{
			"definition": map[string]any{
				"id":          id,
				"name":        "jira-create-flow",
				"version":     "punakawan.workflow/v1",
				"description": "create a jira issue as a workflow step",
				"enabled":     true,
				"revision":    0,
				"steps": []map[string]any{
					{"id": "s1", "capability": cap},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("save_workflow_definition(cap=%q) transport error: %v", cap, err)
	}
	return res
}

// TestSaveWorkflowDefinitionAcceptsCreateJiraIssueCapability is the regression
// guard for bd punokawan-kf1p. The dedicated create_jira_issue MCP tool is
// registered into the capability registry via addTool (tools.go), so a workflow
// step may reference it directly - it is NOT rejected as an unknown capability.
// The original report ("workflow save rejected with 'unknown capability' on
// every step") was against the wrong capability spelling, not a real gap.
func TestSaveWorkflowDefinitionAcceptsCreateJiraIssueCapability(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	res := saveWorkflowDefWithCapability(t, cs, "wf-create-jira", "create_jira_issue")
	if res.IsError {
		t.Fatalf("create_jira_issue should be a valid workflow capability, got error: %+v", res.Content)
	}
}

// TestSaveWorkflowDefinitionNormalizesJiraCreateAliases makes the agent-facing
// save path forgiving: both names an agent can reasonably infer from the
// Atlassian adapter normalize to the portable native tool capability.
func TestSaveWorkflowDefinitionNormalizesJiraCreateAliases(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	for _, cap := range []string{"createJiraIssue", "atlassian.createJiraIssue"} {
		res := saveWorkflowDefWithCapability(t, cs, "wf-"+cap, cap)
		if res.IsError {
			t.Fatalf("cap=%q should normalize to create_jira_issue, got error: %+v", cap, res.Content)
		}
	}
}

func TestSaveWorkflowDefinitionStillRejectsUnknownCapability(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	res := saveWorkflowDefWithCapability(t, cs, "wf-unknown", "definitely_not_a_tool")
	if !res.IsError {
		t.Fatal("unknown capability should be rejected")
	}
	text := ""
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		text = tc.Text
	}
	if !strings.Contains(text, "unknown capability") {
		t.Fatalf("error should mention unknown capability, got: %q", text)
	}
}
