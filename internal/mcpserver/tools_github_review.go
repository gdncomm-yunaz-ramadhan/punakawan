package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/providerwrite"
)

type HydrateGitHubPullRequestInput struct {
	Repository        string `json:"repository"`
	PullRequestNumber int    `json:"pull_request_number"`
}

type HydrateGitHubPullRequestOutput struct {
	Context map[string]any `json:"context"`
}

func hydrateGitHubPullRequestHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, HydrateGitHubPullRequestInput) (*mcp.CallToolResult, HydrateGitHubPullRequestOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in HydrateGitHubPullRequestInput) (*mcp.CallToolResult, HydrateGitHubPullRequestOutput, error) {
		if in.Repository == "" || in.PullRequestNumber <= 0 {
			return nil, HydrateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: hydrate GitHub pull request requires repository and positive pull_request_number")
		}
		gate, err := a.AdapterRegistry.Gate(ctx, "github")
		if err != nil {
			return nil, HydrateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: open GitHub adapter: %w", err)
		}
		runID := fmt.Sprintf("github-pr-%s-%d", in.Repository, in.PullRequestNumber)
		call := func(op string, params map[string]any) (map[string]any, error) {
			raw, err := gate.Call(ctx, runID, op, params)
			if err != nil {
				return nil, err
			}
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			return value, nil
		}
		pr, err := call("github.getPullRequest", map[string]any{"repository": in.Repository, "pullRequestNumber": in.PullRequestNumber})
		if err != nil {
			return nil, HydrateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: fetch GitHub pull request: %w", err)
		}
		normalized, _ := pr["normalized"].(map[string]any)
		headSHA, _ := normalized["headSha"].(string)
		files, err := call("github.getPullRequestFiles", map[string]any{"repository": in.Repository, "pullRequestNumber": in.PullRequestNumber})
		if err != nil {
			return nil, HydrateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: fetch GitHub pull request files: %w", err)
		}
		comments, err := call("github.listPullRequestComments", map[string]any{"repository": in.Repository, "pullRequestNumber": in.PullRequestNumber})
		if err != nil {
			return nil, HydrateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: fetch GitHub pull request comments: %w", err)
		}
		threads, err := call("github.listUnresolvedReviewThreads", map[string]any{"repository": in.Repository, "pullRequestNumber": in.PullRequestNumber})
		if err != nil {
			return nil, HydrateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: fetch GitHub review threads: %w", err)
		}
		out := map[string]any{"pull_request": pr, "files": files, "comments": comments, "unresolved_threads": threads}
		if headSHA != "" {
			checks, err := call("github.getPullRequestChecks", map[string]any{"repository": in.Repository, "ref": headSHA})
			if err != nil {
				return nil, HydrateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: fetch GitHub pull request checks: %w", err)
			}
			out["checks"] = checks
		}
		return nil, HydrateGitHubPullRequestOutput{Context: out}, nil
	}
}

type ProposeGitHubPRReviewInput struct {
	Repository          string           `json:"repository"`
	PullRequestNumber   int              `json:"pull_request_number"`
	HeadSHA             string           `json:"head_sha"`
	Findings            []map[string]any `json:"findings"`
	Body                string           `json:"body"`
	Verdict             string           `json:"verdict"`
	DeliveryExecutionID string           `json:"delivery_execution_id,omitempty"`
	IdempotencyKey      string           `json:"idempotency_key,omitempty"`
}

type ProposeGitHubPRReviewOutput struct {
	Review delivery.GitHubPRReview `json:"review"`
}

func proposeGitHubPRReviewHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ProposeGitHubPRReviewInput) (*mcp.CallToolResult, ProposeGitHubPRReviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ProposeGitHubPRReviewInput) (*mcp.CallToolResult, ProposeGitHubPRReviewOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, ProposeGitHubPRReviewOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		review, err := store.ProposeGitHubPRReview(ctx, key, in.Repository, in.PullRequestNumber, in.HeadSHA, in.Findings, in.Body, in.Verdict, in.DeliveryExecutionID)
		if err != nil {
			return nil, ProposeGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: propose GitHub PR review: %w", err)
		}
		return nil, ProposeGitHubPRReviewOutput{Review: *review}, nil
	}
}

type GetGitHubPRReviewInput struct {
	ReviewID string `json:"review_id"`
}

type GetGitHubPRReviewOutput struct {
	Review delivery.GitHubPRReview `json:"review"`
}

func getGitHubPRReviewHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, GetGitHubPRReviewInput) (*mcp.CallToolResult, GetGitHubPRReviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in GetGitHubPRReviewInput) (*mcp.CallToolResult, GetGitHubPRReviewOutput, error) {
		if strings.TrimSpace(in.ReviewID) == "" {
			return nil, GetGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: get GitHub PR review requires review_id")
		}
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, GetGitHubPRReviewOutput{}, err
		}
		review, err := store.GetGitHubPRReview(ctx, in.ReviewID)
		if err != nil {
			return nil, GetGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: get GitHub PR review: %w", err)
		}
		return nil, GetGitHubPRReviewOutput{Review: *review}, nil
	}
}

type SubmitGitHubPRReviewInput struct {
	ReviewID       string `json:"review_id"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type SubmitGitHubPRReviewOutput struct {
	Review delivery.GitHubPRReview `json:"review"`
}

// submitGitHubPRReviewHandler routes the actual GitHub write through the
// durable outbox (providerwrite.ExecuteNow), attempting it synchronously so
// this tool call still returns a definitive result the way it always has.
// Anything short of an immediate success (a retryable adapter rejection, an
// ambiguous attempt) is treated as this one synchronous submission
// attempt's own failure: the underlying outbox intent is cancelled rather
// than left to retry silently in the background, and the review is marked
// failed with the redacted diagnostic, so a caller sees one definitive
// outcome per call instead of a write that might complete unobserved later.
func submitGitHubPRReviewHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitGitHubPRReviewInput) (*mcp.CallToolResult, SubmitGitHubPRReviewOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SubmitGitHubPRReviewInput) (*mcp.CallToolResult, SubmitGitHubPRReviewOutput, error) {
		if strings.TrimSpace(in.ReviewID) == "" {
			return nil, SubmitGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: submit GitHub PR review requires review_id")
		}
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, SubmitGitHubPRReviewOutput{}, err
		}
		review, err := store.GetGitHubPRReview(ctx, in.ReviewID)
		if err != nil {
			return nil, SubmitGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: get GitHub PR review for submission: %w", err)
		}
		if review.Status == "submitted" {
			return nil, SubmitGitHubPRReviewOutput{Review: *review}, nil
		}
		if review.Status != "proposed" {
			return nil, SubmitGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: submit GitHub PR review: %w", delivery.ErrInvalidState)
		}
		key := strings.TrimSpace(in.IdempotencyKey)
		if key == "" {
			key = delivery.NewID()
		}
		params, err := githubPRReviewAdapterParams(review)
		if err != nil {
			return nil, SubmitGitHubPRReviewOutput{}, resolveGitHubPRReviewFailure(ctx, store, key, review.ID, fmt.Errorf("prepare GitHub PR review: %w", err))
		}
		outboxStore, err := a.OpenOutbox()
		if err != nil {
			return nil, SubmitGitHubPRReviewOutput{}, err
		}
		payload, err := json.Marshal(map[string]any{
			"pull_request_number": params["pullRequestNumber"],
			"head_sha":            params["commitId"],
			"body":                params["body"],
			"event":               params["event"],
			"comments":            params["comments"],
		})
		if err != nil {
			return nil, SubmitGitHubPRReviewOutput{}, resolveGitHubPRReviewFailure(ctx, store, key, review.ID, fmt.Errorf("encode GitHub PR review payload: %w", err))
		}
		resolved, err := providerwrite.ExecuteNow(ctx, outboxStore, a.AdapterRegistry, "github-pr-review-"+review.ID, outbox.Intent{
			OrchestrationID: review.DeliveryExecutionID, AdapterID: "github", Operation: githubCreatePullRequestReviewOperation,
			TargetKey: review.Repository, PayloadJSON: string(payload),
			OperationFingerprint: providerwrite.GitHubReviewFingerprint(review.Repository, review.PullRequestNumber, review.HeadSHA, review.ID),
		})
		if err != nil {
			return nil, SubmitGitHubPRReviewOutput{}, resolveGitHubPRReviewFailure(ctx, store, key, review.ID, fmt.Errorf("enqueue GitHub PR review: %w", err))
		}
		if resolved.Status != outbox.StatusSucceeded {
			diag := resolved.LastErrorRedacted
			if diag == "" {
				diag = "GitHub PR review submission did not complete"
			}
			if _, cancelErr := outboxStore.Cancel(ctx, resolved.ID, "submit_github_pr_review: giving up after one synchronous attempt"); cancelErr != nil {
				return nil, SubmitGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: cancel unresolved GitHub PR review intent: %w", cancelErr)
			}
			return nil, SubmitGitHubPRReviewOutput{}, resolveGitHubPRReviewFailure(ctx, store, key, review.ID, errors.New(diag))
		}
		final, err := store.ResolveGitHubPRReview(ctx, key, review.ID, resolved.ExternalID, "")
		if err != nil {
			return nil, SubmitGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: persist submitted GitHub PR review: %w", err)
		}
		return nil, SubmitGitHubPRReviewOutput{Review: *final}, nil
	}
}

const githubCreatePullRequestReviewOperation = "github.createPullRequestReview"

func resolveGitHubPRReviewFailure(ctx context.Context, store *delivery.Store, key, reviewID string, cause error) error {
	if _, err := store.ResolveGitHubPRReview(ctx, key, reviewID, "", cause.Error()); err != nil {
		return fmt.Errorf("mcpserver: persist failed GitHub PR review: %w", err)
	}
	return fmt.Errorf("mcpserver: submit GitHub PR review: %w", cause)
}

func githubPRReviewAdapterParams(review *delivery.GitHubPRReview) (map[string]any, error) {
	if review == nil || strings.TrimSpace(review.Repository) == "" || review.PullRequestNumber <= 0 || strings.TrimSpace(review.HeadSHA) == "" || strings.TrimSpace(review.Body) == "" {
		return nil, fmt.Errorf("invalid GitHub PR review")
	}
	if review.Verdict != "APPROVE" && review.Verdict != "REQUEST_CHANGES" && review.Verdict != "COMMENT" {
		return nil, fmt.Errorf("invalid GitHub PR review verdict %q", review.Verdict)
	}
	params := map[string]any{
		"repository":        review.Repository,
		"pullRequestNumber": review.PullRequestNumber,
		"body":              review.Body,
		"event":             review.Verdict,
		"commitId":          review.HeadSHA,
	}
	if comments := githubPRReviewInlineComments(review.Findings); len(comments) > 0 {
		params["comments"] = comments
	}
	return params, nil
}

func githubPRReviewInlineComments(findings []map[string]any) []map[string]any {
	comments := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		path, pathOK := githubPRReviewFindingString(finding, "file", "path")
		line, lineOK := githubPRReviewFindingLine(finding, "end_line", "endLine", "line", "start_line", "startLine")
		body := githubPRReviewFindingBody(finding)
		if !pathOK || !lineOK || body == "" {
			continue
		}
		side, sideOK := githubPRReviewFindingString(finding, "side")
		if !sideOK {
			side = "RIGHT"
		} else {
			side = strings.ToUpper(side)
			if side != "LEFT" && side != "RIGHT" {
				continue
			}
		}
		comment := map[string]any{"path": path, "line": line, "side": side, "body": body}
		if startLine, ok := githubPRReviewFindingLine(finding, "start_line", "startLine"); ok && startLine < line {
			comment["startLine"] = startLine
			comment["startSide"] = side
		}
		comments = append(comments, comment)
	}
	return comments
}

func githubPRReviewFindingString(finding map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		value, ok := finding[key]
		if !ok {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		if text = strings.TrimSpace(text); text != "" {
			return text, true
		}
	}
	return "", false
}

func githubPRReviewFindingLine(finding map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		value, ok := finding[key]
		if !ok {
			continue
		}
		var line int
		switch value := value.(type) {
		case int:
			line = value
		case int64:
			line = int(value)
		case float64:
			if value != float64(int(value)) {
				continue
			}
			line = int(value)
		case float32:
			if value != float32(int(value)) {
				continue
			}
			line = int(value)
		case json.Number:
			parsed, err := value.Int64()
			if err != nil {
				continue
			}
			line = int(parsed)
		default:
			continue
		}
		if line > 0 {
			return line, true
		}
	}
	return 0, false
}

func githubPRReviewFindingBody(finding map[string]any) string {
	title, _ := githubPRReviewFindingString(finding, "title")
	explanation, _ := githubPRReviewFindingString(finding, "explanation", "body", "message")
	switch {
	case title != "" && explanation != "":
		return title + "\n\n" + explanation
	case explanation != "":
		return explanation
	default:
		return title
	}
}

