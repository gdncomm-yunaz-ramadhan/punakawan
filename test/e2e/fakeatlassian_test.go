//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// jiraTransition is one workflow transition a fakeAtlassianServer reports
// as available from an issue's current status.
type jiraTransition struct {
	id, name, toStatusID, toStatusName string
}

// jiraWorkflow is the fixed, three-status workflow every issue on a
// fakeAtlassianServer moves through - enough to exercise a start
// transition (To Do -> In Progress) and a completion transition
// (In Progress -> Done) the way a real Jira project's workflow would.
var jiraWorkflow = map[string][]jiraTransition{
	"To Do":       {{"11", "Start Progress", "3", "In Progress"}},
	"In Progress": {{"21", "Stop Progress", "1", "To Do"}, {"31", "Done", "5", "Done"}},
	"Done":        {{"41", "Reopen", "1", "To Do"}},
}

type jiraComment struct{ id, body string }
type jiraWorklogEntry struct {
	id      string
	comment string
	seconds int
}

// jiraIssue is one fake Jira issue's full mutable state: identity fields,
// its comments, and its worklog entries, guarded by the owning server's
// mutex.
type jiraIssue struct {
	key, parentKey, summary, description, status, issueType string
	subtaskKeys                                             []string
	comments                                                []jiraComment
	worklogs                                                []jiraWorklogEntry
}

// fakeAtlassianServer is an in-memory stand-in for the Atlassian REST
// surface internal/jiraintegration, internal/jirahooks, and
// internal/providerwrite call through the "atlassian" adapter. It is a
// real net/http/httptest server - every call arrives as an actual HTTP
// request - just handling a private op-dispatch scheme instead of Jira's
// real REST paths, since no real adapter subprocess runs in this suite.
type fakeAtlassianServer struct {
	mu     sync.Mutex
	issues map[string]*jiraIssue
	seq    int
}

func newFakeAtlassianServer(t *testing.T) (*httptest.Server, *fakeAtlassianServer) {
	t.Helper()
	f := &fakeAtlassianServer{issues: map[string]*jiraIssue{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	return srv, f
}

// addIssue seeds one issue. parentKey is "" for a top-level issue;
// subtaskKeys names the issue's own subtasks by key (seeded separately).
func (f *fakeAtlassianServer) addIssue(key, parentKey, summary, description, status, issueType string, subtaskKeys ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issues[key] = &jiraIssue{
		key: key, parentKey: parentKey, summary: summary, description: description,
		status: status, issueType: issueType, subtaskKeys: subtaskKeys,
	}
}

func (f *fakeAtlassianServer) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d", prefix, f.seq)
}

func (f *fakeAtlassianServer) handle(w http.ResponseWriter, r *http.Request) {
	var params map[string]any
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	op, _ := params["op"].(string)

	f.mu.Lock()
	defer f.mu.Unlock()

	switch op {
	case "atlassian.getJiraIssue":
		f.getJiraIssue(w, params)
	case "atlassian.getTransitionsForJiraIssue":
		f.getTransitions(w, params)
	case "atlassian.getJiraComments":
		f.getComments(w, params)
	case "atlassian.addJiraComment":
		f.addComment(w, params)
	case "atlassian.transitionJiraIssue":
		f.transition(w, params)
	case "atlassian.addWorklog":
		f.addWorklog(w, params)
	case "atlassian.listJiraWorklogs":
		f.listWorklogs(w, params)
	default:
		http.Error(w, "fake atlassian: unimplemented op "+op, http.StatusNotImplemented)
	}
}

func (f *fakeAtlassianServer) issue(w http.ResponseWriter, params map[string]any) (*jiraIssue, bool) {
	key, _ := params["issueIdOrKey"].(string)
	issue, ok := f.issues[key]
	if !ok {
		http.Error(w, "fake atlassian: unknown issue "+key, http.StatusNotFound)
		return nil, false
	}
	return issue, true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeAtlassianServer) getJiraIssue(w http.ResponseWriter, params map[string]any) {
	issue, ok := f.issue(w, params)
	if !ok {
		return
	}
	var parent any
	if issue.parentKey != "" {
		parent = map[string]any{"key": issue.parentKey}
	}
	subtasks := make([]map[string]any, 0, len(issue.subtaskKeys))
	for _, key := range issue.subtaskKeys {
		summary := key
		if child, ok := f.issues[key]; ok {
			summary = child.summary
		}
		subtasks = append(subtasks, map[string]any{"key": key, "summary": summary})
	}
	writeJSON(w, map[string]any{"normalized": map[string]any{
		"key": issue.key, "summary": issue.summary, "description": issue.description,
		"status": issue.status, "issueType": issue.issueType, "parent": parent, "subtasks": subtasks,
	}})
}

func (f *fakeAtlassianServer) getTransitions(w http.ResponseWriter, params map[string]any) {
	issue, ok := f.issue(w, params)
	if !ok {
		return
	}
	transitions := make([]map[string]any, 0)
	for _, t := range jiraWorkflow[issue.status] {
		transitions = append(transitions, map[string]any{
			"id": t.id, "name": t.name, "toStatus": map[string]any{"id": t.toStatusID, "name": t.toStatusName},
		})
	}
	writeJSON(w, map[string]any{"transitions": transitions})
}

func (f *fakeAtlassianServer) getComments(w http.ResponseWriter, params map[string]any) {
	issue, ok := f.issue(w, params)
	if !ok {
		return
	}
	comments := make([]map[string]any, 0, len(issue.comments))
	for _, c := range issue.comments {
		comments = append(comments, map[string]any{"id": c.id, "body": c.body})
	}
	writeJSON(w, map[string]any{"comments": comments})
}

func (f *fakeAtlassianServer) addComment(w http.ResponseWriter, params map[string]any) {
	issue, ok := f.issue(w, params)
	if !ok {
		return
	}
	body, _ := params["commentBody"].(string)
	id := f.nextID("comment")
	issue.comments = append(issue.comments, jiraComment{id: id, body: body})
	writeJSON(w, map[string]any{"commentId": id})
}

func (f *fakeAtlassianServer) transition(w http.ResponseWriter, params map[string]any) {
	issue, ok := f.issue(w, params)
	if !ok {
		return
	}
	transitionID, _ := params["transitionId"].(string)
	for _, t := range jiraWorkflow[issue.status] {
		if t.id == transitionID {
			issue.status = t.toStatusName
			writeJSON(w, map[string]any{"ok": true})
			return
		}
	}
	http.Error(w, "fake atlassian: transition "+transitionID+" not available from "+issue.status, http.StatusConflict)
}

func (f *fakeAtlassianServer) addWorklog(w http.ResponseWriter, params map[string]any) {
	issue, ok := f.issue(w, params)
	if !ok {
		return
	}
	comment, _ := params["comment"].(string)
	seconds := 0
	if n, ok := params["timeSpentSeconds"].(float64); ok {
		seconds = int(n)
	}
	id := f.nextID("worklog")
	issue.worklogs = append(issue.worklogs, jiraWorklogEntry{id: id, comment: comment, seconds: seconds})
	writeJSON(w, map[string]any{"worklogId": id})
}

func (f *fakeAtlassianServer) listWorklogs(w http.ResponseWriter, params map[string]any) {
	issue, ok := f.issue(w, params)
	if !ok {
		return
	}
	worklogs := make([]map[string]any, 0, len(issue.worklogs))
	for _, wl := range issue.worklogs {
		worklogs = append(worklogs, map[string]any{"id": wl.id, "comment": wl.comment})
	}
	writeJSON(w, map[string]any{"worklogs": worklogs})
}
