package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
)

const fakeGitHubReviewAdapterEnv = "PUNAKAWAN_TEST_GITHUB_REVIEW_ADAPTER"

// githubCreatePullRequestReviewOperation names the adapter operation
// githubintegration.Service.SubmitReview enqueues - kept here as a test
// fixture constant since this test's fake adapter needs to name it too,
// even though the production call site no longer references it directly.
const githubCreatePullRequestReviewOperation = "github.createPullRequestReview"

// githubGetPullRequestOperation names the read op
// githubintegration.Service.SubmitReview issues first, to confirm the pull
// request's head SHA still matches the proposal before submitting a review.
const githubGetPullRequestOperation = "github.getPullRequest"

func TestGitHubPRReviewHandlersProposeThenSubmitDirectly(t *testing.T) {
	// Execution proceeds straight from proposed to submitted: side_effect no
	// longer implies an approval gate a caller must clear first.
	a := newTestApp(t)
	a.AdapterRegistry = adapters.NewRegistry(map[string]adapters.AdapterSpec{
		"github": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestGitHubPRReviewFakeAdapter"},
			Env:     []string{fakeGitHubReviewAdapterEnv + "=1"},
		},
	})
	ctx := context.Background()
	findings := []map[string]any{{
		"title":       "Rounding loses cents",
		"explanation": "Use decimal rounding before persisting the refund.",
		"file":        "src/refund.go",
		"start_line":  12,
		"end_line":    13,
	}}

	_, proposed, err := proposeGitHubPRReviewHandler(a)(ctx, nil, ProposeGitHubPRReviewInput{
		Repository:          "acme/widgets",
		PullRequestNumber:   42,
		HeadSHA:             "abc123",
		Findings:            findings,
		Body:                "Please fix the rounding behavior.",
		Verdict:             "REQUEST_CHANGES",
		DeliveryExecutionID: "execution-1",
	})
	if err != nil {
		t.Fatalf("propose handler: %v", err)
	}

	_, second, err := proposeGitHubPRReviewHandler(a)(ctx, nil, ProposeGitHubPRReviewInput{
		Repository:          "acme/widgets",
		PullRequestNumber:   43,
		HeadSHA:             "def456",
		Body:                "Looks good.",
		Verdict:             "COMMENT",
		DeliveryExecutionID: "execution-1",
	})
	if err != nil {
		t.Fatalf("second propose handler: %v", err)
	}

	_, submitted, err := submitGitHubPRReviewHandler(a)(ctx, nil, SubmitGitHubPRReviewInput{ReviewID: proposed.Review.ID})
	if err != nil {
		t.Fatalf("submit handler: %v", err)
	}
	if submitted.Review.Status != "submitted" || submitted.Review.ExternalReviewID != "701" {
		t.Fatalf("submitted review = %+v, want submitted external review 701", submitted.Review)
	}

	_, fetched, err := getGitHubPRReviewHandler(a)(ctx, nil, GetGitHubPRReviewInput{ReviewID: proposed.Review.ID})
	if err != nil {
		t.Fatalf("get handler: %v", err)
	}
	if fetched.Review.Status != "submitted" || fetched.Review.ExternalReviewID != "701" {
		t.Fatalf("retrieved review = %+v, want persisted submission result", fetched.Review)
	}
	if _, _, err := submitGitHubPRReviewHandler(a)(ctx, nil, SubmitGitHubPRReviewInput{ReviewID: second.Review.ID}); err == nil {
		t.Fatal("second submit handler succeeded, want fake adapter failure")
	}
	_, failed, err := getGitHubPRReviewHandler(a)(ctx, nil, GetGitHubPRReviewInput{ReviewID: second.Review.ID})
	if err != nil {
		t.Fatalf("get failed review handler: %v", err)
	}
	if failed.Review.Status != "failed" || failed.Review.Failure == "" {
		t.Fatalf("failed review = %+v, want persisted failed submission result", failed.Review)
	}

}

func TestGitHubPRReviewFakeAdapter(t *testing.T) {
	if os.Getenv(fakeGitHubReviewAdapterEnv) != "1" {
		return
	}

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
			writeFakeGitHubReviewAdapterResult(output, request.ID, map[string]any{
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
					githubCreatePullRequestReviewOperation: map[string]any{
						"side_effect":  true,
						"description":  "test fixture operation",
						"input_schema": map[string]any{"type": "object"},
					},
					githubGetPullRequestOperation: map[string]any{
						"side_effect":  false,
						"description":  "test fixture operation",
						"input_schema": map[string]any{"type": "object"},
					},
				},
			})
		case "initialize":
			writeFakeGitHubReviewAdapterResult(output, request.ID, map[string]any{"ok": true})
		case "execute":
			var params map[string]any
			if err := json.Unmarshal(request.Params, &params); err != nil {
				writeFakeGitHubReviewAdapterError(output, request.ID, "invalid execute params")
				continue
			}
			if params["op"] == githubGetPullRequestOperation {
				writeFakeGitHubReviewAdapterResult(output, request.ID, map[string]any{
					"normalized": map[string]any{"headSha": fakeGitHubReviewCurrentHeadSHA(params)},
				})
				continue
			}
			if !fakeGitHubReviewSubmissionIsExpected(params) {
				writeFakeGitHubReviewAdapterError(output, request.ID, "unexpected review submission")
				continue
			}
			writeFakeGitHubReviewAdapterResult(output, request.ID, map[string]any{"ok": true, "reviewId": "701"})
		case "shutdown":
			writeFakeGitHubReviewAdapterResult(output, request.ID, map[string]any{"ok": true})
			return
		default:
			writeFakeGitHubReviewAdapterError(output, request.ID, "unexpected method")
		}
	}
}

func writeFakeGitHubReviewAdapterResult(output *json.Encoder, id int64, result any) {
	_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeFakeGitHubReviewAdapterError(output *json.Encoder, id int64, message string) {
	_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -1, "message": message}})
}

// fakeGitHubReviewCurrentHeadSHA echoes back the head SHA each of this
// test's two proposals was made against, so SubmitReview's own freshness
// pre-flight (github.getPullRequest) sees an unmoved head for both and
// never itself the reason a submission fails.
func fakeGitHubReviewCurrentHeadSHA(params map[string]any) string {
	switch params["pullRequestNumber"] {
	case float64(42):
		return "abc123"
	case float64(43):
		return "def456"
	default:
		return ""
	}
}

func fakeGitHubReviewSubmissionIsExpected(params map[string]any) bool {
	if params["op"] != githubCreatePullRequestReviewOperation || params["repository"] != "acme/widgets" || params["pullRequestNumber"] != float64(42) || params["commitId"] != "abc123" || params["event"] != "REQUEST_CHANGES" {
		return false
	}
	comments, ok := params["comments"].([]any)
	if !ok || len(comments) != 1 {
		return false
	}
	comment, ok := comments[0].(map[string]any)
	return ok && comment["path"] == "src/refund.go" && comment["line"] == float64(13) && comment["startLine"] == float64(12) && comment["side"] == "RIGHT" && comment["startSide"] == "RIGHT" && comment["body"] == "Rounding loses cents\n\nUse decimal rounding before persisting the refund."
}
