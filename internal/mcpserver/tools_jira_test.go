package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	m.Operations["atlassian.getJiraIssue"] = readOp
	m.Operations["atlassian.createIssueLink"] = approvalRequiredOp
	m.Operations["atlassian.editJiraIssueFields"] = approvalRequiredOp
	m.Operations["atlassian.createJiraIssue"] = approvalRequiredOp
	m.Operations["atlassian.listJiraBoards"] = readOp
	m.Operations["atlassian.listJiraSprints"] = readOp
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

// --- list_jira_subtasks ----------------------------------------------------

func TestListJiraSubtasksReturnsChildren(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	fc.responses = map[string]string{
		"atlassian.getJiraIssue": `{"normalized":{
			"key":"PAY-100","summary":"Refund epic","status":"In Progress",
			"subtasks":[
				{"key":"PAY-101","summary":"Backend refund endpoint","status":"To Do"},
				{"key":"PAY-102","summary":"Refund UI","status":"In Progress"}
			]
		}}`,
	}

	in := ListJiraSubtasksInput{IssueIdOrKey: "PAY-100", RequestedBy: "petruk"}
	out, err := listJiraSubtasks(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("listJiraSubtasks: %v", err)
	}
	if out.ParentKey != "PAY-100" || out.ParentSummary != "Refund epic" || out.ParentStatus != "In Progress" {
		t.Errorf("parent = %+v, want PAY-100/Refund epic/In Progress", out)
	}
	if out.SubtaskCount != 2 || len(out.Subtasks) != 2 {
		t.Fatalf("SubtaskCount = %d / %+v, want 2", out.SubtaskCount, out.Subtasks)
	}
	if out.Subtasks[0].Key != "PAY-101" || out.Subtasks[0].Status != "To Do" {
		t.Errorf("Subtasks[0] = %+v, want PAY-101/To Do", out.Subtasks[0])
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want one getJiraIssue call (a read, no approval)", fc.calls)
	}
	c := fc.calls[0]
	if c["op"] != "atlassian.getJiraIssue" {
		t.Errorf("op = %v, want atlassian.getJiraIssue", c["op"])
	}
	if c["issueIdOrKey"] != "PAY-100" {
		t.Errorf("issueIdOrKey = %v, want PAY-100", c["issueIdOrKey"])
	}
}

func TestListJiraSubtasksNoChildren(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	// A leaf issue with no subtasks: adapter omits the field entirely.
	// (also asserts a read needs no prior approval — no approveOp call above.)
	fc.responses = map[string]string{
		"atlassian.getJiraIssue": `{"normalized":{"key":"PAY-200","summary":"Leaf task","status":"To Do"}}`,
	}
	in := ListJiraSubtasksInput{IssueIdOrKey: "PAY-200", RequestedBy: "petruk"}
	out, err := listJiraSubtasks(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("listJiraSubtasks (leaf) should not require approval: %v", err)
	}
	if out.SubtaskCount != 0 || len(out.Subtasks) != 0 {
		t.Errorf("SubtaskCount = %d, want 0", out.SubtaskCount)
	}
}

func TestListJiraSubtasksRequiresIssueKey(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := ListJiraSubtasksInput{RequestedBy: "petruk"}
	if _, err := listJiraSubtasks(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when issue_id_or_key is empty")
	}
}

func TestListJiraSubtasksRejectsInvalidRequestedBy(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := ListJiraSubtasksInput{IssueIdOrKey: "PAY-100", RequestedBy: "boss"}
	if _, err := listJiraSubtasks(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error for an invalid requested_by")
	}
}

// --- jira_find_sprint --------------------------------------------------------

func TestJiraFindSprintByBoardId(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	fc.responses = map[string]string{
		"atlassian.listJiraSprints": `{"sprints":[
			{"id":1,"name":"Sprint 1","state":"closed","boardId":42},
			{"id":2,"name":"Sprint 2","state":"active","boardId":42}
		]}`,
	}

	in := JiraFindSprintInput{BoardId: 42, RequestedBy: "petruk"}
	out, err := jiraFindSprint(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraFindSprint: %v", err)
	}
	if len(out.BoardIds) != 1 || out.BoardIds[0] != 42 {
		t.Fatalf("BoardIds = %+v, want [42]", out.BoardIds)
	}
	if out.Count != 2 || len(out.Sprints) != 2 {
		t.Fatalf("Sprints = %+v, want 2", out.Sprints)
	}
	if out.Sprints[1].Id != 2 || out.Sprints[1].State != "active" || out.Sprints[1].BoardId != 42 {
		t.Errorf("Sprints[1] = %+v, want id=2 state=active board_id=42", out.Sprints[1])
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one listJiraSprints call (a read, no approval)", fc.calls)
	}
	c := fc.calls[0]
	if c["op"] != "atlassian.listJiraSprints" || c["boardId"] != 42 {
		t.Errorf("call = %+v, want listJiraSprints boardId=42", c)
	}
}

func TestJiraFindSprintByProjectKeyResolvesBoards(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	fc.responses = map[string]string{
		"atlassian.listJiraBoards":  `{"boards":[{"id":42,"name":"PAY board","type":"scrum"}]}`,
		"atlassian.listJiraSprints": `{"sprints":[{"id":7,"name":"PAY Sprint 7","state":"active","boardId":42}]}`,
	}
	in := JiraFindSprintInput{ProjectKey: "PAY", RequestedBy: "petruk"}
	out, err := jiraFindSprint(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraFindSprint: %v", err)
	}
	if len(out.BoardIds) != 1 || out.BoardIds[0] != 42 {
		t.Fatalf("BoardIds = %+v, want [42]", out.BoardIds)
	}
	if len(out.Sprints) != 1 || out.Sprints[0].Id != 7 {
		t.Fatalf("Sprints = %+v, want one sprint id 7", out.Sprints)
	}
	if len(fc.calls) != 2 {
		t.Fatalf("calls = %+v, want listJiraBoards then listJiraSprints", fc.calls)
	}
	if fc.calls[0]["op"] != "atlassian.listJiraBoards" || fc.calls[0]["projectKeyOrId"] != "PAY" {
		t.Errorf("calls[0] = %+v, want listJiraBoards projectKeyOrId=PAY", fc.calls[0])
	}
	if fc.calls[1]["op"] != "atlassian.listJiraSprints" || fc.calls[1]["boardId"] != 42 {
		t.Errorf("calls[1] = %+v, want listJiraSprints boardId=42", fc.calls[1])
	}
}

func TestJiraFindSprintFiltersByStateAndQuery(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	fc.responses = map[string]string{
		"atlassian.listJiraSprints": `{"sprints":[
			{"id":1,"name":"Alpha Sprint","state":"active","boardId":42},
			{"id":2,"name":"Beta Sprint","state":"active","boardId":42}
		]}`,
	}
	in := JiraFindSprintInput{BoardId: 42, State: "Active", Query: "alpha", RequestedBy: "petruk"}
	out, err := jiraFindSprint(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("jiraFindSprint: %v", err)
	}
	if len(out.Sprints) != 1 || out.Sprints[0].Name != "Alpha Sprint" {
		t.Fatalf("Sprints = %+v, want only Alpha Sprint", out.Sprints)
	}
	if fc.calls[0]["state"] != "active" {
		t.Errorf("call state = %v, want lowercased active", fc.calls[0]["state"])
	}
}

func TestJiraFindSprintRequiresBoardOrProject(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraFindSprintInput{RequestedBy: "petruk"}
	if _, err := jiraFindSprint(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when neither board_id nor project_key is set")
	}
}

func TestJiraFindSprintRejectsInvalidState(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraFindSprintInput{BoardId: 42, State: "bogus", RequestedBy: "petruk"}
	if _, err := jiraFindSprint(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error for an invalid state filter")
	}
}

func TestJiraFindSprintNeedsNoApproval(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraFindSprintInput{BoardId: 42, RequestedBy: "petruk"}
	if _, err := jiraFindSprint(context.Background(), nil, gate, in); err != nil {
		t.Fatalf("jiraFindSprint (read) should not require approval: %v", err)
	}
}

func TestJiraFindSprintErrorsWhenProjectHasNoBoards(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	// No response registered for listJiraBoards -> the fake caller's default
	// `{"ok":true}` decodes to zero boards, which must surface as an error
	// rather than silently returning no sprints.
	in := JiraFindSprintInput{ProjectKey: "NOPE", RequestedBy: "petruk"}
	if _, err := jiraFindSprint(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when project resolves to no scrum boards")
	}
}

func TestJiraFindSprintRejectsInvalidRequestedBy(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := JiraFindSprintInput{BoardId: 42, RequestedBy: "boss"}
	if _, err := jiraFindSprint(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error for an invalid requested_by")
	}
}

// TestJiraFindSprintListedOverMCPTransport exercises the real MCP wire
// protocol (server_test.go's connect/newTestApp helpers), not just the
// fake-gate unit tests above: it confirms jira_find_sprint is actually
// registered and reachable via tools/list, with the input schema fields this
// tool depends on. A full connect+CallTool round trip against a real
// atlassian adapter is not exercised here - unlike call_adapter_operation's
// prototype-adapter end-to-end test, there is no fake atlassian subprocess to
// spawn, and this machine's real global Punakawan config (not a test
// fixture) is what MergeAdapters would otherwise resolve "atlassian" through,
// which would make the test's outcome depend on host state rather than this
// change. See the fake-gate tests above for jira_find_sprint's actual
// behavior, matching how every sibling jira_* tool in this file is tested.
func TestJiraFindSprintListedOverMCPTransport(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	res, err := cs.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "jira_find_sprint" {
			continue
		}
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal jira_find_sprint input schema: %v", err)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatalf("decode jira_find_sprint input schema: %v", err)
		}
		if _, ok := schema.Properties["board_id"]; !ok {
			t.Errorf("jira_find_sprint input schema missing board_id property")
		}
		if _, ok := schema.Properties["project_key"]; !ok {
			t.Errorf("jira_find_sprint input schema missing project_key property")
		}
		return
	}
	t.Fatal("jira_find_sprint is not registered in tools/list")
}

// --- list_jira_linked_issues -----------------------------------------------

func TestListJiraLinkedIssuesReturnsLinks(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	fc.responses = map[string]string{
		"atlassian.getJiraIssue": `{"normalized":{"key":"PAY-1","links":[
			{"direction":"outward","relationship":"blocks","issue":{"key":"PAY-2","summary":"Downstream","status":"To Do","issueType":"Task"}},
			{"direction":"inward","relationship":"is blocked by","issue":{"key":"PAY-3","summary":"Upstream","status":"Done","issueType":"Bug"}}
		]}}`,
	}
	in := ListJiraLinkedIssuesInput{IssueIdOrKey: "PAY-1", RequestedBy: "petruk"}
	out, err := listJiraLinkedIssues(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("listJiraLinkedIssues: %v", err)
	}
	if out.IssueKey != "PAY-1" || out.Count != 2 {
		t.Fatalf("out = %+v, want PAY-1/count 2", out)
	}
	if out.Links[0].Relationship != "blocks" || out.Links[0].Key != "PAY-2" || out.Links[0].IssueType != "Task" {
		t.Errorf("Links[0] = %+v", out.Links[0])
	}
	if fc.calls[0]["op"] != "atlassian.getJiraIssue" {
		t.Errorf("op = %v, want getJiraIssue (read)", fc.calls[0]["op"])
	}
}

func TestListJiraLinkedIssuesRequiresIssueKey(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	if _, err := listJiraLinkedIssues(context.Background(), nil, gate, ListJiraLinkedIssuesInput{RequestedBy: "petruk"}); err == nil {
		t.Fatal("expected an error when issue_id_or_key is empty")
	}
}

// --- list_jira_comments ----------------------------------------------------

func TestListJiraCommentsReturnsComments(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	fc.responses = map[string]string{
		"atlassian.getJiraComments": `{"comments":[
			{"id":"1001","author":"Ada","body":"First","created":"2026-01-01T00:00:00Z"},
			{"id":"1002","author":"Alan","body":"Second","created":"2026-01-02T00:00:00Z"}
		],"page":{"startAt":0,"returned":2,"total":5}}`,
	}
	in := ListJiraCommentsInput{IssueIdOrKey: "PAY-1", MaxResults: 2, RequestedBy: "petruk"}
	out, err := listJiraComments(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("listJiraComments: %v", err)
	}
	if out.Returned != 2 || len(out.Comments) != 2 {
		t.Fatalf("out = %+v, want 2 comments", out)
	}
	if out.Total == nil || *out.Total != 5 {
		t.Fatalf("Total = %v, want 5 (more pages exist)", out.Total)
	}
	if out.Comments[0].Author != "Ada" || out.Comments[0].Body != "First" {
		t.Errorf("Comments[0] = %+v", out.Comments[0])
	}
	c := fc.calls[0]
	if c["op"] != "atlassian.getJiraComments" || c["maxResults"] != 2 {
		t.Errorf("call = %+v, want getJiraComments maxResults=2 (read)", c)
	}
}

func TestListJiraCommentsRequiresIssueKey(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	if _, err := listJiraComments(context.Background(), nil, gate, ListJiraCommentsInput{RequestedBy: "petruk"}); err == nil {
		t.Fatal("expected an error when issue_id_or_key is empty")
	}
}

// --- add_jira_comment ------------------------------------------------------

func TestAddJiraCommentPostsAndReturnsId(t *testing.T) {
	m := jiraNativeToolsManifest()
	m.Operations["atlassian.addJiraComment"] = m.Operations["atlassian.createIssueLink"] // side_effect + approval:required
	gate, fc := newJiraClarifyTestGateWithManifest(t, m)
	approveOp(t, gate, "run-1", "atlassian.addJiraComment")
	fc.responses = map[string]string{"atlassian.addJiraComment": `{"ok":true,"commentId":"9001"}`}

	in := AddJiraCommentInput{RunId: "run-1", IssueIdOrKey: "PAY-1", Body: "LGTM", RequestedBy: "petruk"}
	out, err := addJiraComment(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("addJiraComment: %v", err)
	}
	if !out.Posted || out.CommentId != "9001" {
		t.Fatalf("out = %+v, want Posted=true CommentId=9001", out)
	}
	c := fc.calls[0]
	if c["op"] != "atlassian.addJiraComment" || c["commentBody"] != "LGTM" {
		t.Errorf("call = %+v, want addJiraComment commentBody=LGTM", c)
	}
}

func TestAddJiraCommentFailsWithoutApproval(t *testing.T) {
	m := jiraNativeToolsManifest()
	m.Operations["atlassian.addJiraComment"] = m.Operations["atlassian.createIssueLink"]
	gate, _ := newJiraClarifyTestGateWithManifest(t, m)
	in := AddJiraCommentInput{RunId: "run-1", IssueIdOrKey: "PAY-1", Body: "LGTM", RequestedBy: "petruk"}
	if _, err := addJiraComment(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when addJiraComment has not been approved")
	}
}

func TestAddJiraCommentRequiresBody(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, jiraNativeToolsManifest())
	in := AddJiraCommentInput{IssueIdOrKey: "PAY-1", RequestedBy: "petruk"}
	if _, err := addJiraComment(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when body is empty")
	}
}
