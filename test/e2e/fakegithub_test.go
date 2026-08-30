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

// githubPullRequest is one fake pull request's full state, keyed by
// repository + number.
type githubPullRequest struct {
	number                 int
	repository             string
	title, body            string
	headBranch, baseBranch string
	headSHA                string
	state                  string
	url                    string
}

// fakeGitHubServer is an in-memory stand-in for the GitHub REST surface
// internal/githubintegration and internal/providerwrite call through the
// "github" adapter. Like fakeAtlassianServer, it is a real
// net/http/httptest server dispatching a private op scheme instead of
// GitHub's real REST paths.
type fakeGitHubServer struct {
	mu  sync.Mutex
	prs map[string]*githubPullRequest // key: repository + "#" + number
	seq int
}

func newFakeGitHubServer(t *testing.T) (*httptest.Server, *fakeGitHubServer) {
	t.Helper()
	f := &fakeGitHubServer{prs: map[string]*githubPullRequest{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	return srv, f
}

func prKey(repository string, number int) string {
	return fmt.Sprintf("%s#%d", repository, number)
}

func (f *fakeGitHubServer) findByBranches(repository, head, base string) *githubPullRequest {
	for _, pr := range f.prs {
		if pr.repository == repository && pr.headBranch == head && pr.baseBranch == base {
			return pr
		}
	}
	return nil
}

func (f *fakeGitHubServer) handle(w http.ResponseWriter, r *http.Request) {
	var params map[string]any
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	op, _ := params["op"].(string)

	f.mu.Lock()
	defer f.mu.Unlock()

	switch op {
	case "github.createPullRequest":
		f.createPullRequest(w, params)
	case "github.findPullRequest":
		f.findPullRequest(w, params)
	case "github.getPullRequest":
		f.getPullRequest(w, params)
	default:
		http.Error(w, "fake github: unimplemented op "+op, http.StatusNotImplemented)
	}
}

// normalizedJSON renders pr the way every github.* read/write response
// this fake serves wraps its payload: under a top-level "normalized" key,
// matching internal/providerwrite's and internal/githubintegration's own
// decode targets (result.Normalized.Number/Url/HeadSha).
func normalizedJSON(pr *githubPullRequest) map[string]any {
	if pr == nil {
		return map[string]any{"normalized": nil}
	}
	return map[string]any{"normalized": map[string]any{
		"number":     pr.number,
		"url":        pr.url,
		"headSha":    pr.headSHA,
		"headBranch": pr.headBranch,
		"baseBranch": pr.baseBranch,
		"title":      pr.title,
		"body":       pr.body,
		"state":      pr.state,
	}}
}

func (f *fakeGitHubServer) createPullRequest(w http.ResponseWriter, params map[string]any) {
	repository, _ := params["repository"].(string)
	head, _ := params["headBranch"].(string)
	base, _ := params["baseBranch"].(string)
	if existing := f.findByBranches(repository, head, base); existing != nil {
		writeJSON(w, normalizedJSON(existing))
		return
	}
	f.seq++
	pr := &githubPullRequest{
		number:     f.seq,
		repository: repository,
		title:      stringField(params, "title"),
		body:       stringField(params, "body"),
		headBranch: head,
		baseBranch: base,
		headSHA:    fmt.Sprintf("sha-%s-%d", head, f.seq),
		state:      "open",
		url:        fmt.Sprintf("https://github.example/%s/pull/%d", repository, f.seq),
	}
	f.prs[prKey(repository, pr.number)] = pr
	writeJSON(w, normalizedJSON(pr))
}

func (f *fakeGitHubServer) findPullRequest(w http.ResponseWriter, params map[string]any) {
	repository, _ := params["repository"].(string)
	head, _ := params["headBranch"].(string)
	base, _ := params["baseBranch"].(string)
	writeJSON(w, normalizedJSON(f.findByBranches(repository, head, base)))
}

func (f *fakeGitHubServer) getPullRequest(w http.ResponseWriter, params map[string]any) {
	repository, _ := params["repository"].(string)
	number := 0
	if n, ok := params["pullRequestNumber"].(float64); ok {
		number = int(n)
	}
	pr, ok := f.prs[prKey(repository, number)]
	if !ok {
		http.Error(w, "fake github: unknown pull request", http.StatusNotFound)
		return
	}
	writeJSON(w, normalizedJSON(pr))
}

func stringField(params map[string]any, key string) string {
	v, _ := params[key].(string)
	return v
}
