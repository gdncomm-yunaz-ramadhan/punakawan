package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// jiraNativeToolsManifest extends the shared atlassian test manifest with the
// operations the punokawan-t6y native tools invoke: a read (searchJiraUsers),
// an approval-gated link write (createIssueLink), and editJiraIssueFields
// (reused by story-points and assign).
func jiraNativeToolsManifest() protocol.AdapterManifest {
	m := atlassianTestManifest()
	// The map value is an anonymous struct, so copy the shape of an existing
	// entry (as progressTestManifest does) rather than naming the type.
	approvalRequiredOp := m.Operations["atlassian.addJiraComment"] // side_effect + approval:required
	readOp := m.Operations["atlassian.getTransitionsForJiraIssue"] // side_effect false, no approval
	m.Operations["atlassian.searchJiraUsers"] = readOp
	m.Operations["atlassian.createIssueLink"] = approvalRequiredOp
	m.Operations["atlassian.editJiraIssueFields"] = approvalRequiredOp
	return m
}

// --- jira_search_user ------------------------------------------------------

func TestJiraSearchUserReturnsAccountIds(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	fc.responses = map[string]string{
		"atlassian.searchJiraUsers": `{"users":[
			{"accountId":"acc-1","displayName":"Ada Lovelace","emailAddress":"ada@example.com","active":true},
			{"accountId":"acc-2","displayName":"Alan Turing","emailAddress":"alan@example.com","active":false}
		]}`,
	}

	in := JiraSearchUserInput{Query: "a", MaxResults: 10, RequestedBy: "petruk"}
	out, err := jiraSearchUser(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraSearchUser: %v", err)
	}
	if len(out.Users) != 2 {
		t.Fatalf("Users = %+v, want 2", out.Users)
	}
	if out.Users[0].AccountId != "acc-1" || out.Users[0].DisplayName != "Ada Lovelace" || !out.Users[0].Active {
		t.Errorf("Users[0] = %+v, want acc-1/Ada Lovelace/active", out.Users[0])
	}

	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one searchJiraUsers call (a read, no approval)", fc.calls)
	}
	c := fc.calls[0]
	if c["op"] != "atlassian.searchJiraUsers" {
		t.Errorf("op = %v, want atlassian.searchJiraUsers", c["op"])
	}
	if c["query"] != "a" {
		t.Errorf("query = %v, want a", c["query"])
	}
	if c["maxResults"] != 10 {
		t.Errorf("maxResults = %v, want 10", c["maxResults"])
	}
}

func TestJiraSearchUserNeedsNoApproval(t *testing.T) {
	// The read must succeed without any approval having been granted for the run.
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraSearchUserInput{Query: "ada", RequestedBy: "petruk"}
	if _, err := jiraSearchUser(context.Background(), nil, gate, in); err != nil {
		t.Fatalf("jiraSearchUser (read) should not require approval: %v", err)
	}
}

func TestJiraSearchUserRequiresQuery(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraSearchUserInput{RequestedBy: "petruk"}
	if _, err := jiraSearchUser(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when query is empty")
	}
}

func TestJiraSearchUserRejectsInvalidRequestedBy(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraSearchUserInput{Query: "ada", RequestedBy: "boss"}
	if _, err := jiraSearchUser(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error for an invalid requested_by")
	}
}

// --- jira_link_issues ------------------------------------------------------

func TestJiraLinkIssuesCreatesLink(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	approveOp(t, gate, "run-1", "atlassian.createIssueLink")

	in := JiraLinkIssuesInput{RunId: "run-1", InwardIssue: "PAY-2", OutwardIssue: "PAY-1", LinkType: "Blocks", RequestedBy: "petruk"}
	out, err := jiraLinkIssues(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraLinkIssues: %v", err)
	}
	if !out.Linked || out.LinkType != "Blocks" {
		t.Fatalf("out = %+v, want Linked=true LinkType=Blocks", out)
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want one createIssueLink call", fc.calls)
	}
	c := fc.calls[0]
	if c["op"] != "atlassian.createIssueLink" {
		t.Errorf("op = %v, want atlassian.createIssueLink", c["op"])
	}
	if c["linkType"] != "Blocks" || c["inwardIssueKey"] != "PAY-2" || c["outwardIssueKey"] != "PAY-1" {
		t.Errorf("params = %+v, want linkType=Blocks inwardIssueKey=PAY-2 outwardIssueKey=PAY-1", c)
	}
}

func TestJiraLinkIssuesDefaultsToRelates(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	approveOp(t, gate, "run-1", "atlassian.createIssueLink")

	in := JiraLinkIssuesInput{RunId: "run-1", InwardIssue: "PAY-2", OutwardIssue: "PAY-1", RequestedBy: "petruk"}
	out, err := jiraLinkIssues(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraLinkIssues: %v", err)
	}
	if out.LinkType != "Relates" {
		t.Errorf("LinkType = %q, want Relates (default)", out.LinkType)
	}
	if fc.calls[0]["linkType"] != "Relates" {
		t.Errorf("linkType = %v, want Relates", fc.calls[0]["linkType"])
	}
}

func TestJiraLinkIssuesFailsWithoutApproval(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraLinkIssuesInput{RunId: "run-1", InwardIssue: "PAY-2", OutwardIssue: "PAY-1", RequestedBy: "petruk"}
	if _, err := jiraLinkIssues(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when createIssueLink has not been approved")
	}
}

func TestJiraLinkIssuesRequiresBothIssues(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraLinkIssuesInput{RunId: "run-1", InwardIssue: "PAY-2", RequestedBy: "petruk"}
	if _, err := jiraLinkIssues(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when outward_issue is empty")
	}
}

// --- jira_set_story_points -------------------------------------------------

func TestJiraSetStoryPointsUsesDefaultField(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	approveOp(t, gate, "run-1", "atlassian.editJiraIssueFields")

	in := JiraSetStoryPointsInput{RunId: "run-1", IssueIdOrKey: "PAY-1", StoryPoints: 5, RequestedBy: "petruk"}
	out, err := jiraSetStoryPoints(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraSetStoryPoints: %v", err)
	}
	if !out.Updated || out.FieldId != defaultStoryPointsFieldId || out.StoryPoints != 5 {
		t.Fatalf("out = %+v, want Updated=true FieldId=%s StoryPoints=5", out, defaultStoryPointsFieldId)
	}
	c := fc.calls[0]
	if c["op"] != "atlassian.editJiraIssueFields" || c["issueIdOrKey"] != "PAY-1" {
		t.Errorf("call = %+v, want editJiraIssueFields on PAY-1", c)
	}
	fields, _ := c["fields"].(map[string]any)
	if fields[defaultStoryPointsFieldId] != 5.0 {
		t.Errorf("fields = %+v, want %s=5", fields, defaultStoryPointsFieldId)
	}
}

func TestJiraSetStoryPointsHonorsFieldOverride(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	approveOp(t, gate, "run-1", "atlassian.editJiraIssueFields")

	in := JiraSetStoryPointsInput{RunId: "run-1", IssueIdOrKey: "PAY-1", StoryPoints: 8, StoryPointsFieldId: "customfield_10099", RequestedBy: "petruk"}
	out, err := jiraSetStoryPoints(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraSetStoryPoints: %v", err)
	}
	if out.FieldId != "customfield_10099" {
		t.Errorf("FieldId = %q, want customfield_10099", out.FieldId)
	}
	fields, _ := fc.calls[0]["fields"].(map[string]any)
	if fields["customfield_10099"] != 8.0 {
		t.Errorf("fields = %+v, want customfield_10099=8", fields)
	}
}

func TestJiraSetStoryPointsFailsWithoutApproval(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraSetStoryPointsInput{RunId: "run-1", IssueIdOrKey: "PAY-1", StoryPoints: 5, RequestedBy: "petruk"}
	if _, err := jiraSetStoryPoints(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when editJiraIssueFields has not been approved")
	}
}

// --- jira_assign_issue -----------------------------------------------------

func TestJiraAssignIssueSetsAssignee(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	approveOp(t, gate, "run-1", "atlassian.editJiraIssueFields")

	in := JiraAssignIssueInput{RunId: "run-1", IssueIdOrKey: "PAY-1", AccountId: "acc-1", RequestedBy: "petruk"}
	out, err := jiraAssignIssue(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraAssignIssue: %v", err)
	}
	if !out.Assigned || out.AccountId != "acc-1" {
		t.Fatalf("out = %+v, want Assigned=true AccountId=acc-1", out)
	}
	fields, _ := fc.calls[0]["fields"].(map[string]any)
	assignee, _ := fields["assignee"].(map[string]any)
	if assignee["accountId"] != "acc-1" {
		t.Errorf("assignee = %+v, want accountId=acc-1", assignee)
	}
}

func TestJiraAssignIssueRequiresAccountId(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraAssignIssueInput{RunId: "run-1", IssueIdOrKey: "PAY-1", RequestedBy: "petruk"}
	if _, err := jiraAssignIssue(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when account_id is empty")
	}
}

func TestJiraAssignIssueFailsWithoutApproval(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraAssignIssueInput{RunId: "run-1", IssueIdOrKey: "PAY-1", AccountId: "acc-1", RequestedBy: "petruk"}
	if _, err := jiraAssignIssue(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when editJiraIssueFields has not been approved")
	}
}

// --- lightweight one-off mode ----------------------------------------------

func TestResolveRunIDDefaultsToOneoff(t *testing.T) {
	if got := resolveRunID(""); got != oneoffRunID {
		t.Errorf("resolveRunID(\"\") = %q, want %q", got, oneoffRunID)
	}
	if got := resolveRunID("  "); got != oneoffRunID {
		t.Errorf("resolveRunID(blank) = %q, want %q", got, oneoffRunID)
	}
	if got := resolveRunID("run-42"); got != "run-42" {
		t.Errorf("resolveRunID(\"run-42\") = %q, want run-42", got)
	}
}

func TestJiraLinkIssuesOneoffRunUsesDefaultRunID(t *testing.T) {
	// With no run_id, the write is gated under the oneoff run: approving that
	// run is enough, no create_workflow_run ceremony required.
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	approveOp(t, gate, oneoffRunID, "atlassian.createIssueLink")

	in := JiraLinkIssuesInput{InwardIssue: "PAY-2", OutwardIssue: "PAY-1", RequestedBy: "petruk"}
	out, err := jiraLinkIssues(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraLinkIssues (oneoff): %v", err)
	}
	if !out.Linked {
		t.Fatal("Linked = false, want true under the oneoff run")
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want one createIssueLink call", fc.calls)
	}
}
