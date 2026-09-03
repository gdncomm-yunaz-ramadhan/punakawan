package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
)

const fakeGitHubCreatePRAdapterEnv = "PUNAKAWAN_TEST_GITHUB_CREATE_PR_ADAPTER"

// githubCreatePullRequestOperation names the adapter operation
// githubintegration.Service.CreatePullRequest enqueues.
const githubCreatePullRequestOperation = "github.createPullRequest"

func newCreatePRTestApp(t *testing.T) *app.App {
	t.Helper()
	a := newTestApp(t)
	a.AdapterRegistry = adapters.NewRegistry(map[string]adapters.AdapterSpec{
		"github": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestGitHubCreatePRFakeAdapter"},
			Env:     []string{fakeGitHubCreatePRAdapterEnv + "=1"},
		},
	})
	return a
}

func TestCreateGitHubPullRequestOpensOnceAndDedupesRetries(t *testing.T) {
	a := newCreatePRTestApp(t)
	ctx := context.Background()

	in := CreateGitHubPullRequestInput{
		Repository: "acme/widgets", BaseBranch: "main", HeadBranch: "feature-x", Title: "Add feature x",
	}

	_, first, err := createGitHubPullRequestHandler(a)(ctx, nil, in)
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	if first.Status != "created" || first.Number != 42 || first.URL != "https://github.com/acme/widgets/pull/42" || first.Repository != "acme/widgets" {
		t.Fatalf("first result = %+v, want created PR #42", first)
	}

	// The fake adapter fails any second execution of this operation, so a
	// repeated call succeeding here proves ExecuteNow's existing dedup
	// short-circuit resolved it from the durable intent instead of
	// invoking the adapter again.
	_, second, err := createGitHubPullRequestHandler(a)(ctx, nil, in)
	if err != nil {
		t.Fatalf("repeated create handler: %v", err)
	}
	if second.Status != "created" || second.Number != 42 || second.URL != first.URL {
		t.Fatalf("repeated result = %+v, want the same PR #42 without a second adapter call", second)
	}
}

func TestCreateGitHubPullRequestSurfacesAdapterFailure(t *testing.T) {
	a := newCreatePRTestApp(t)
	ctx := context.Background()

	_, _, err := createGitHubPullRequestHandler(a)(ctx, nil, CreateGitHubPullRequestInput{
		Repository: "acme/widgets", BaseBranch: "main", HeadBranch: "boom", Title: "Should fail",
	})
	if err == nil {
		t.Fatal("create handler succeeded, want the fake adapter's rejection surfaced as an error")
	}
}

func TestCreateGitHubPullRequestRequiresBranchesAndTitleBeforeTouchingAdapter(t *testing.T) {
	a := newTestApp(t)
	a.AdapterRegistry = adapters.NewRegistry(map[string]adapters.AdapterSpec{
		"github": {
			Command: os.Args[0],
			// A -test.run pattern that can never match any test name: if
			// validation fails to short-circuit before the adapter
			// registry is touched, spawning this "adapter" fails loudly
			// instead of the test silently passing.
			Args: []string{"-test.run=^$"},
		},
	})
	ctx := context.Background()

	for _, in := range []CreateGitHubPullRequestInput{
		{Repository: "acme/widgets", HeadBranch: "feature-x", Title: "t"},
		{Repository: "acme/widgets", BaseBranch: "main", Title: "t"},
		{Repository: "acme/widgets", BaseBranch: "main", HeadBranch: "feature-x"},
		{Repository: "acme/widgets", BaseBranch: "  ", HeadBranch: " ", Title: " "},
	} {
		if _, _, err := createGitHubPullRequestHandler(a)(ctx, nil, in); err == nil {
			t.Fatalf("create handler(%+v) succeeded, want a validation error", in)
		}
	}
}

func TestGitHubCreatePRFakeAdapter(t *testing.T) {
	if os.Getenv(fakeGitHubCreatePRAdapterEnv) != "1" {
		return
	}

	executed := false
	input := bufio.NewScanner(os.Stdin)
	output := json.NewEncoder(os.Stdout)
	for input.Scan() {
		var request struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(input.Bytes(), &request); err != nil {
			return
		}
		switch request.Method {
		case "capabilities":
			writeFakeGitHubCreatePRAdapterResult(output, request.ID, map[string]any{
				"id":       "github",
				"name":     "Fake GitHub",
				"version":  "0.0.0",
				"protocol": "punakawan.adapter/v1",
				"runtime":  "node",
				"provides": []string{"github"},
				"permissions": map[string]any{
					"network":    map[string]any{"hosts": []string{}},
					"filesystem": map[string]any{"read": []string{}, "write": []string{}},
					"secrets":    []string{},
				},
				"operations": map[string]any{
					githubCreatePullRequestOperation: map[string]any{
						"side_effect":  true,
						"description":  "test fixture operation",
						"input_schema": map[string]any{"type": "object"},
					},
				},
			})
		case "initialize":
			writeFakeGitHubCreatePRAdapterResult(output, request.ID, map[string]any{"ok": true})
		case "execute":
			var params map[string]any
			if err := json.Unmarshal(request.Params, &params); err != nil {
				writeFakeGitHubCreatePRAdapterError(output, request.ID, "invalid execute params")
				continue
			}
			if params["op"] != githubCreatePullRequestOperation {
				writeFakeGitHubCreatePRAdapterError(output, request.ID, "unexpected operation")
				continue
			}
			if params["headBranch"] == "boom" {
				writeFakeGitHubCreatePRAdapterError(output, request.ID, "simulated github failure")
				continue
			}
			if executed {
				writeFakeGitHubCreatePRAdapterError(output, request.ID, "unexpected second execution of github.createPullRequest")
				continue
			}
			executed = true
			writeFakeGitHubCreatePRAdapterResult(output, request.ID, map[string]any{
				"normalized": map[string]any{"number": 42, "url": "https://github.com/acme/widgets/pull/42"},
			})
		case "shutdown":
			writeFakeGitHubCreatePRAdapterResult(output, request.ID, map[string]any{"ok": true})
			return
		default:
			writeFakeGitHubCreatePRAdapterError(output, request.ID, "unexpected method")
		}
	}
}

func writeFakeGitHubCreatePRAdapterResult(output *json.Encoder, id int64, result any) {
	_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeFakeGitHubCreatePRAdapterError(output *json.Encoder, id int64, message string) {
	_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -1, "message": message}})
}
