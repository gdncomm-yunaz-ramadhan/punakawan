package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/githubintegration"
	"github.com/ygrip/punakawan/pkg/protocol"
)

type HydrateGitHubPullRequestInput struct {
	Repository        string `json:"repository"`
	PullRequestNumber int    `json:"pull_request_number"`
}

type HydrateGitHubPullRequestOutput struct {
	// Status is "hydrated", or "needs_input" when the repository named
	// could not be settled without a decision only a human can make.
	Status     string                  `json:"status,omitempty"`
	NeedsInput *protocol.NeedUserInput `json:"needs_input,omitempty"`
	// Repository is the exact owner/repo this call read, which is not
	// always the string the caller passed: a bare name or an omitted one
	// is resolved here.
	Repository string         `json:"repository,omitempty"`
	Context    map[string]any `json:"context,omitempty"`
}

func hydrateGitHubPullRequestHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, HydrateGitHubPullRequestInput) (*mcp.CallToolResult, HydrateGitHubPullRequestOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in HydrateGitHubPullRequestInput) (*mcp.CallToolResult, HydrateGitHubPullRequestOutput, error) {
		if in.PullRequestNumber <= 0 {
			return nil, HydrateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: hydrate GitHub pull request requires a positive pull_request_number")
		}
		outboxStore, err := a.OpenOutbox()
		if err != nil {
			return nil, HydrateGitHubPullRequestOutput{}, err
		}
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, HydrateGitHubPullRequestOutput{}, err
		}
		repository, needsInput, err := resolveGitHubRepository(ctx, a, store, in.Repository)
		if err != nil {
			return nil, HydrateGitHubPullRequestOutput{}, err
		}
		if needsInput != nil {
			return nil, HydrateGitHubPullRequestOutput{Status: "needs_input", NeedsInput: needsInput}, nil
		}
		svc := githubintegration.NewService(a.AdapterRegistry, outboxStore, gitHubOrgResolver(a, store))
		runID := fmt.Sprintf("github-pr-%s-%d", repository, in.PullRequestNumber)
		out, err := svc.HydratePullRequest(ctx, runID, repository, in.PullRequestNumber)
		if err != nil {
			return nil, HydrateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: hydrate GitHub pull request: %w", err)
		}
		return nil, HydrateGitHubPullRequestOutput{Status: "hydrated", Repository: repository, Context: out}, nil
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
	// Status is "proposed", or "needs_input" when the repository could
	// not be settled without a decision only a human can make.
	Status     string                   `json:"status,omitempty"`
	NeedsInput *protocol.NeedUserInput  `json:"needs_input,omitempty"`
	Review     *delivery.GitHubPRReview `json:"review,omitempty"`
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
		// Resolving before the proposal is persisted is the point: a
		// review stored against an unresolvable repository used to be
		// accepted here and fail only at submit time, by which point the
		// findings it carries have nowhere to go.
		repository, needsInput, err := resolveGitHubRepository(ctx, a, store, in.Repository)
		if err != nil {
			return nil, ProposeGitHubPRReviewOutput{}, err
		}
		if needsInput != nil {
			return nil, ProposeGitHubPRReviewOutput{Status: "needs_input", NeedsInput: needsInput}, nil
		}
		review, err := store.ProposeGitHubPRReview(ctx, key, repository, in.PullRequestNumber, in.HeadSHA, in.Findings, in.Body, in.Verdict, in.DeliveryExecutionID)
		if err != nil {
			return nil, ProposeGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: propose GitHub PR review: %w", err)
		}
		return nil, ProposeGitHubPRReviewOutput{Status: "proposed", Review: review}, nil
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

// submitGitHubPRReviewHandler routes the actual GitHub write through
// githubintegration.Service.SubmitReview, attempting it synchronously so
// this tool call still returns a definitive result the way it always has.
// A stale proposal (the pull request's head moved since it was proposed)
// or anything short of an immediate success (a retryable adapter
// rejection, an unresolved ambiguous attempt) is treated as this one
// synchronous submission attempt's own failure: the review is marked
// failed with the redacted diagnostic, so a caller sees one definitive
// outcome per call instead of a write that might complete unobserved
// later.
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
		comments, _ := params["comments"].([]map[string]any)
		svc := githubintegration.NewService(a.AdapterRegistry, outboxStore, gitHubOrgResolver(a, store))
		externalID, err := svc.SubmitReview(ctx, githubintegration.SubmitReviewRequest{
			RunID: "github-pr-review-" + review.ID, Repository: review.Repository, PullRequestNumber: review.PullRequestNumber,
			HeadSHA: review.HeadSHA, Body: review.Body, Event: review.Verdict, Comments: comments, ReviewID: review.ID,
		})
		if err != nil {
			return nil, SubmitGitHubPRReviewOutput{}, resolveGitHubPRReviewFailure(ctx, store, key, review.ID, err)
		}
		final, err := store.ResolveGitHubPRReview(ctx, key, review.ID, externalID, "")
		if err != nil {
			return nil, SubmitGitHubPRReviewOutput{}, fmt.Errorf("mcpserver: persist submitted GitHub PR review: %w", err)
		}
		return nil, SubmitGitHubPRReviewOutput{Review: *final}, nil
	}
}

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
