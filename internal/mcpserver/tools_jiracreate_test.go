package mcpserver

import (
	"context"
	"testing"
)

func TestCreateJiraIssueCreatesIssue(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	approveOp(t, gate, "run-1", "atlassian.createJiraIssue")
	fc.responses = map[string]string{
		"atlassian.createJiraIssue": `{"normalized":{"key":"PAY-42","summary":"Login button does nothing on Safari","status":"Open","issueType":"Bug","source":{"uri":"https://example.atlassian.net/browse/PAY-42"}}}`,
	}

	in := CreateJiraIssueInput{
		RunId:         "run-1",
		ProjectKey:    "PAY",
		IssueTypeName: "Bug",
		Summary:       "Login button does nothing on Safari",
		Description:   "Clicking Login on Safari does not submit the form.",
		ParentKey:     "PAY-1",
		AdditionalFields: map[string]any{
			"priority": map[string]any{"name": "High"},
		},
		RequestedBy: "petruk",
	}
	out, err := createJiraIssueTool(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("createJiraIssueTool: %v", err)
	}
	if !out.Created || out.Key != "PAY-42" || out.Status != "Open" || out.IssueType != "Bug" {
		t.Fatalf("out = %+v, want Created=true Key=PAY-42 Status=Open IssueType=Bug", out)
	}
	if out.Url != "https://example.atlassian.net/browse/PAY-42" {
		t.Errorf("Url = %q, want the issue's browse URL", out.Url)
	}

	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one createJiraIssue call", fc.calls)
	}
	c := fc.calls[0]
	if c["op"] != "atlassian.createJiraIssue" {
		t.Errorf("op = %v, want atlassian.createJiraIssue", c["op"])
	}
	if c["projectKey"] != "PAY" || c["issueTypeName"] != "Bug" || c["summary"] != in.Summary {
		t.Errorf("params = %+v, want projectKey=PAY issueTypeName=Bug summary=%q", c, in.Summary)
	}
	if c["description"] != in.Description {
		t.Errorf("description = %v, want %q", c["description"], in.Description)
	}
	if c["parent"] != "PAY-1" {
		t.Errorf("parent = %v, want PAY-1", c["parent"])
	}
	if _, ok := c["additionalFields"]; !ok {
		t.Errorf("expected additionalFields to be passed through, got params %+v", c)
	}
}

func TestCreateJiraIssueOmitsOptionalParamsWhenNotGiven(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	approveOp(t, gate, "run-1", "atlassian.createJiraIssue")
	fc.responses = map[string]string{
		"atlassian.createJiraIssue": `{"normalized":{"key":"PAY-43","summary":"Add logout confirmation","status":"Open","issueType":"Task"}}`,
	}

	in := CreateJiraIssueInput{RunId: "run-1", ProjectKey: "PAY", IssueTypeName: "Task", Summary: "Add logout confirmation", RequestedBy: "petruk"}
	out, err := createJiraIssueTool(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("createJiraIssueTool: %v", err)
	}
	if out.Key != "PAY-43" || out.Url != "" {
		t.Fatalf("out = %+v, want Key=PAY-43 and no Url", out)
	}

	c := fc.calls[0]
	for _, key := range []string{"description", "parent", "additionalFields"} {
		if _, ok := c[key]; ok {
			t.Errorf("expected %q to be omitted when not given, got params %+v", key, c)
		}
	}
}

func TestCreateJiraIssueRequiresProjectKey(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := CreateJiraIssueInput{IssueTypeName: "Bug", Summary: "Something broke", RequestedBy: "petruk"}
	if _, err := createJiraIssueTool(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when project_key is empty")
	}
}

func TestCreateJiraIssueRequiresIssueTypeName(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := CreateJiraIssueInput{ProjectKey: "PAY", Summary: "Something broke", RequestedBy: "petruk"}
	if _, err := createJiraIssueTool(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when issue_type_name is empty")
	}
}

func TestCreateJiraIssueRequiresSummary(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := CreateJiraIssueInput{ProjectKey: "PAY", IssueTypeName: "Bug", RequestedBy: "petruk"}
	if _, err := createJiraIssueTool(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when summary is empty")
	}
}

func TestCreateJiraIssueRejectsInvalidRequestedBy(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := CreateJiraIssueInput{ProjectKey: "PAY", IssueTypeName: "Bug", Summary: "Something broke", RequestedBy: "boss"}
	if _, err := createJiraIssueTool(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error for an invalid requested_by")
	}
}

func TestCreateJiraIssueFailsWithoutApproval(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := CreateJiraIssueInput{RunId: "run-1", ProjectKey: "PAY", IssueTypeName: "Bug", Summary: "Something broke", RequestedBy: "petruk"}
	if _, err := createJiraIssueTool(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when createJiraIssue has not been approved")
	}
}
