//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/providerwrite"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// httpCaller implements adapters.Gate's unexported caller seam
// (Call(ctx, method, params) (json.RawMessage, error)) by issuing a real
// HTTP POST to a fake provider's httptest server for every adapter
// "execute" call - production code (jiraintegration, githubintegration,
// jirahooks, providerwrite) drives an actual HTTP round trip exactly the
// way it would drive a real adapter's REST client, just against a fake
// server instead of Atlassian/GitHub.
type httpCaller struct {
	url string

	mu       sync.Mutex
	loseOnce map[string]int
}

// loseResponseOnce makes the next call to op perform its effect against
// the fake server as normal, but report providerwrite.ErrResponseLost
// instead of the real response - simulating a dropped connection or
// client-side read timeout discovered only after the remote write already
// applied.
func (c *httpCaller) loseResponseOnce(op string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loseOnce == nil {
		c.loseOnce = map[string]int{}
	}
	c.loseOnce[op]++
}

func (c *httpCaller) takeLoseResponse(op string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loseOnce[op] > 0 {
		c.loseOnce[op]--
		return true
	}
	return false
}

func (c *httpCaller) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	merged, _ := params.(map[string]any)
	op, _ := merged["op"].(string)
	body, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("httpCaller: encode request for %s: %w", op, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("httpCaller: build request for %s: %w", op, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpCaller: %s: %w", op, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("httpCaller: read response for %s: %w", op, err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("httpCaller: %s: %s", op, string(raw))
	}
	if c.takeLoseResponse(op) {
		return nil, providerwrite.ErrResponseLost
	}
	return json.RawMessage(raw), nil
}

// fakeRegistry resolves the "atlassian" and "github" adapter ids to Gates
// wrapping httpCallers pointed at this test's fake provider servers,
// implementing the same Gate(ctx, adapterID) (*adapters.Gate, error) seam
// *adapters.Registry does - the substitution point internal/jiraintegration,
// internal/githubintegration, internal/jirahooks, and internal/providerwrite
// each document for exactly this reason, so no real adapter subprocess is
// ever spawned by this package's tests.
type fakeRegistry struct {
	atlassian *httpCaller
	github    *httpCaller

	// atlassianServer/githubServer are the same fake providers atlassian/
	// github talk to over HTTP, exposed directly so a test can seed and
	// inspect their in-memory state (addIssue, findByBranches, ...)
	// without a second HTTP round trip.
	atlassianServer *fakeAtlassianServer
	githubServer    *fakeGitHubServer

	mu    sync.Mutex
	gates map[string]*adapters.Gate
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	atlassianSrv, atlassianState := newFakeAtlassianServer(t)
	githubSrv, githubState := newFakeGitHubServer(t)
	return &fakeRegistry{
		atlassian:       &httpCaller{url: atlassianSrv.URL},
		github:          &httpCaller{url: githubSrv.URL},
		atlassianServer: atlassianState,
		githubServer:    githubState,
		gates:           map[string]*adapters.Gate{},
	}
}

func (r *fakeRegistry) Gate(ctx context.Context, adapterID string) (*adapters.Gate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gates[adapterID]; ok {
		return g, nil
	}
	var g *adapters.Gate
	switch adapterID {
	case "atlassian":
		g = adapters.NewGate("atlassian", atlassianManifest(), r.atlassian)
	case "github":
		g = adapters.NewGate("github", githubManifest(), r.github)
	default:
		return nil, fmt.Errorf("fakeRegistry: unknown adapter %q", adapterID)
	}
	r.gates[adapterID] = g
	return g, nil
}

// permissiveSchema accepts any object payload, so this harness never has
// to keep a per-operation JSON Schema in sync with every field name the
// production call sites happen to send - Gate's own payload validation
// still runs, it just always passes.
func permissiveSchema() protocol.AdapterManifestOperationsValueInputSchema {
	return protocol.AdapterManifestOperationsValueInputSchema{"type": "object"}
}

func atlassianManifest() protocol.AdapterManifest {
	return protocol.AdapterManifest{
		Id: "atlassian", Name: "atlassian", Version: "0.0.0-e2e", Protocol: "punakawan.adapter/v1",
		Runtime: protocol.AdapterManifestRuntimeNode, Provides: []string{"jira"},
		Permissions: protocol.AdapterManifestPermissions{
			Network:    protocol.AdapterManifestPermissionsNetwork{Hosts: []string{}},
			Filesystem: protocol.AdapterManifestPermissionsFilesystem{Read: []string{}, Write: []string{}},
			Secrets:    []string{},
		},
		Operations: protocol.AdapterManifestOperations{
			"atlassian.getJiraIssue":               {SideEffect: false, Description: "Fetch a Jira issue.", InputSchema: permissiveSchema()},
			"atlassian.getTransitionsForJiraIssue": {SideEffect: false, Description: "List an issue's available transitions.", InputSchema: permissiveSchema()},
			"atlassian.getJiraComments":            {SideEffect: false, Description: "List an issue's comments.", InputSchema: permissiveSchema()},
			"atlassian.listJiraWorklogs":           {SideEffect: false, Description: "List an issue's worklogs.", InputSchema: permissiveSchema()},
			"atlassian.addJiraComment":             {SideEffect: true, Description: "Add a comment to an issue.", InputSchema: permissiveSchema()},
			"atlassian.transitionJiraIssue":        {SideEffect: true, Description: "Transition an issue.", InputSchema: permissiveSchema()},
			"atlassian.addWorklog":                 {SideEffect: true, Description: "Add a worklog to an issue.", InputSchema: permissiveSchema()},
		},
	}
}

func githubManifest() protocol.AdapterManifest {
	return protocol.AdapterManifest{
		Id: "github", Name: "github", Version: "0.0.0-e2e", Protocol: "punakawan.adapter/v1",
		Runtime: protocol.AdapterManifestRuntimeNode, Provides: []string{"github"},
		Permissions: protocol.AdapterManifestPermissions{
			Network:    protocol.AdapterManifestPermissionsNetwork{Hosts: []string{}},
			Filesystem: protocol.AdapterManifestPermissionsFilesystem{Read: []string{}, Write: []string{}},
			Secrets:    []string{},
		},
		Operations: protocol.AdapterManifestOperations{
			"github.createPullRequest": {SideEffect: true, Description: "Open a pull request.", InputSchema: permissiveSchema()},
			"github.findPullRequest":   {SideEffect: false, Description: "Find a pull request by head/base branch.", InputSchema: permissiveSchema()},
			"github.getPullRequest":    {SideEffect: false, Description: "Fetch a pull request.", InputSchema: permissiveSchema()},
		},
	}
}
