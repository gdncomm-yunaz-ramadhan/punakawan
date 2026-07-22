package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/prreview"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ReviewPrInput is review_pr's input, per
// punakawan-architecture-enhancement-plan.md §8.2. review_pr is reactive:
// it must only run after an explicit user instruction to review this
// specific PR (§8.2's Valid/Invalid Triggers), never in response to a PR
// being discovered, CI failing, or any other automatic signal.
type ReviewPrInput struct {
	RunId             string `json:"run_id"`
	RepoId            string `json:"repo_id"`
	PullRequestNumber int    `json:"pull_request_number"`

	// ExplicitTrigger MUST be true only if a human explicitly asked to
	// review this specific PR (e.g. "review PR 42", "check this PR", "is
	// the open PR safe to merge"). Never set this true in response to a PR
	// being discovered, CI failing, a reviewer commenting, or any other
	// automatic/reactive signal (§8.2's invalid triggers) - review_pr
	// refuses to run otherwise.
	ExplicitTrigger bool `json:"explicit_trigger"`

	IncludeExistingComments bool `json:"include_existing_comments,omitempty"`

	RequestedBy string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
}

// PrSummary is the tool-facing shape of a normalized GitHub PR (mirrors
// packages/github-adapter/src/normalize.ts's NormalizedPullRequest).
type PrSummary struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"`
	Draft     bool   `json:"draft"`
	Merged    bool   `json:"merged"`
	Mergeable *bool  `json:"mergeable,omitempty"`
	BaseRef   string `json:"baseRef"`
	HeadRef   string `json:"headRef"`
	HeadSha   string `json:"headSha"`
	Author    string `json:"author,omitempty"`
	Url       string `json:"url,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// PrFile is the tool-facing shape of one changed file in a PR's diff.
type PrFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch,omitempty"`
}

// PrCheck is the tool-facing shape of one CI check run.
type PrCheck struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	Url        string `json:"url,omitempty"`
}

// PrComment is the tool-facing shape of one PR comment (review-line or
// general issue-level).
type PrComment struct {
	Id        string `json:"id"`
	Kind      string `json:"kind"`
	Author    string `json:"author,omitempty"`
	Body      string `json:"body"`
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// ReviewPrOutput is review_pr's output: the fetched PR context bundle.
// review_pr does not itself review anything (ADR-0016: punakawan never
// reasons) - the calling agent builds Gareng/Petruk review capsules
// (request_capsule), collects independent findings, has Bagong verify them
// against this output's Files/Checks, and finally calls
// submit_pr_review_findings with Semar's deduplicated result.
type ReviewPrOutput struct {
	PullRequest PrSummary   `json:"pull_request"`
	Files       []PrFile    `json:"files"`
	Checks      []PrCheck   `json:"checks"`
	Comments    []PrComment `json:"comments,omitempty"`
}

func reviewPrHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReviewPrInput) (*mcp.CallToolResult, ReviewPrOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ReviewPrInput) (*mcp.CallToolResult, ReviewPrOutput, error) {
		if !in.ExplicitTrigger {
			return nil, ReviewPrOutput{}, fmt.Errorf("mcpserver: review_pr refuses to run without explicit_trigger=true (§8.2): only call this after a human explicitly asked to review this specific PR, never in response to a PR being discovered, CI failing, or any other automatic signal")
		}

		repoPath, err := a.RepoPath(in.RepoId)
		if err != nil {
			return nil, ReviewPrOutput{}, fmt.Errorf("mcpserver: resolve repository %q: %w", in.RepoId, err)
		}
		slug, gate, err := resolveGithubRepo(ctx, a.Inspector, a.AdapterRegistry, repoPath)
		if err != nil {
			return nil, ReviewPrOutput{}, err
		}
		requestedBy := protocol.ApprovalRecordRequestedBy(in.RequestedBy)

		out, err := fetchPrContext(ctx, req, gate, slug, in.RunId, in.PullRequestNumber, in.IncludeExistingComments, requestedBy)
		return nil, out, err
	}
}

// fetchPrContext is reviewPrHandler's core logic, split out so it can be
// tested against a Gate built from a fake adapter caller instead of a real
// spawned GitHub adapter process.
func fetchPrContext(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, slug, runID string, prNumber int, includeComments bool, requestedBy protocol.ApprovalRecordRequestedBy) (ReviewPrOutput, error) {
	var out ReviewPrOutput

	prRaw, err := invokeAdapterOperation(ctx, req, gate, runID, "github.getPullRequest", map[string]any{
		"repository": slug, "pullRequestNumber": prNumber,
	}, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: fetch PR metadata: %w", err)
	}
	var prResult struct {
		Normalized PrSummary `json:"normalized"`
	}
	if err := json.Unmarshal(prRaw, &prResult); err != nil {
		return out, fmt.Errorf("mcpserver: decode PR metadata: %w", err)
	}
	out.PullRequest = prResult.Normalized

	filesRaw, err := invokeAdapterOperation(ctx, req, gate, runID, "github.getPullRequestFiles", map[string]any{
		"repository": slug, "pullRequestNumber": prNumber,
	}, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: fetch PR files: %w", err)
	}
	var filesResult struct {
		Normalized []PrFile `json:"normalized"`
	}
	if err := json.Unmarshal(filesRaw, &filesResult); err != nil {
		return out, fmt.Errorf("mcpserver: decode PR files: %w", err)
	}
	out.Files = nonNilFiles(filesResult.Normalized)

	checksRaw, err := invokeAdapterOperation(ctx, req, gate, runID, "github.getPullRequestChecks", map[string]any{
		"repository": slug, "ref": out.PullRequest.HeadSha,
	}, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: fetch PR checks: %w", err)
	}
	var checksResult struct {
		Normalized []PrCheck `json:"normalized"`
	}
	if err := json.Unmarshal(checksRaw, &checksResult); err != nil {
		return out, fmt.Errorf("mcpserver: decode PR checks: %w", err)
	}
	out.Checks = nonNilChecks(checksResult.Normalized)

	if includeComments {
		commentsRaw, err := invokeAdapterOperation(ctx, req, gate, runID, "github.listPullRequestComments", map[string]any{
			"repository": slug, "pullRequestNumber": prNumber,
		}, requestedBy)
		if err != nil {
			return out, fmt.Errorf("mcpserver: fetch PR comments: %w", err)
		}
		var commentsResult struct {
			Normalized []PrComment `json:"normalized"`
		}
		if err := json.Unmarshal(commentsRaw, &commentsResult); err != nil {
			return out, fmt.Errorf("mcpserver: decode PR comments: %w", err)
		}
		out.Comments = commentsResult.Normalized
	}

	return out, nil
}

func nonNilFiles(s []PrFile) []PrFile {
	if s == nil {
		return []PrFile{}
	}
	return s
}

func nonNilChecks(s []PrCheck) []PrCheck {
	if s == nil {
		return []PrCheck{}
	}
	return s
}

// SubmitPrReviewFindingsInput is submit_pr_review_findings's input:
// Semar's deduplicated, prioritized result from review_pr's pipeline
// (§8.2's "return final review" step).
type SubmitPrReviewFindingsInput struct {
	RunId             string                   `json:"run_id"`
	RepoId            string                   `json:"repo_id"`
	PullRequestNumber int                      `json:"pull_request_number"`
	Findings          []protocol.ReviewFinding `json:"findings"`
}

// SubmitPrReviewFindingsOutput is submit_pr_review_findings's output.
type SubmitPrReviewFindingsOutput struct {
	Findings []protocol.ReviewFinding `json:"findings"`
}

func submitPrReviewFindingsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitPrReviewFindingsInput) (*mcp.CallToolResult, SubmitPrReviewFindingsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitPrReviewFindingsInput) (*mcp.CallToolResult, SubmitPrReviewFindingsOutput, error) {
		findings := in.Findings
		if findings == nil {
			findings = []protocol.ReviewFinding{}
		}

		rec := prreview.Record{
			RunId:             in.RunId,
			RepoId:            in.RepoId,
			PullRequestNumber: in.PullRequestNumber,
			Findings:          findings,
			CreatedAt:         time.Now().UTC(),
		}
		if err := a.PrReviews.Append(rec); err != nil {
			return nil, SubmitPrReviewFindingsOutput{}, fmt.Errorf("mcpserver: persist PR review findings: %w", err)
		}
		return nil, SubmitPrReviewFindingsOutput{Findings: findings}, nil
	}
}
