package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// FetchUnresolvedPrCommentsInput is fetch_unresolved_pr_comments's input,
// per punakawan-architecture-enhancement-plan.md §8.3. fix_pr_review is
// reactive: it must only run after an explicit user instruction to fix
// this PR's review comments (§8.3's Valid/Invalid Triggers), never in
// response to a reviewer commenting, CI failing, or review_pr finishing.
type FetchUnresolvedPrCommentsInput struct {
	RunId             string `json:"run_id"`
	RepoId            string `json:"repo_id"`
	PullRequestNumber int    `json:"pull_request_number"`

	// ExplicitTrigger MUST be true only if a human explicitly asked to fix
	// this PR's review comments (e.g. "fix the review comments on PR 42",
	// "address unresolved review threads"). Never set this true in
	// response to a reviewer commenting, changes being requested, CI
	// failing, or review_pr finishing (§8.3's invalid triggers) -
	// fetch_unresolved_pr_comments refuses to run otherwise. Classifying
	// which of the returned comments are applicable/already_resolved/
	// stale/conflicting/requires_clarification/major_change_required
	// (§8.3's ReviewCommentStatus) is the calling agent's judgment, not
	// something this tool determines.
	ExplicitTrigger bool `json:"explicit_trigger"`

	RequestedBy string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
}

// PrReviewThread is one still-open review thread and its comments.
type PrReviewThread struct {
	// Id is GitHub's GraphQL node id for this thread - pass it to
	// resolve_review_thread's thread_id, not any individual comment's id.
	Id       string      `json:"id"`
	Comments []PrComment `json:"comments"`
}

// FetchUnresolvedPrCommentsOutput is fetch_unresolved_pr_comments's output.
type FetchUnresolvedPrCommentsOutput struct {
	Threads []PrReviewThread `json:"threads"`
}

func fetchUnresolvedPrCommentsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, FetchUnresolvedPrCommentsInput) (*mcp.CallToolResult, FetchUnresolvedPrCommentsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in FetchUnresolvedPrCommentsInput) (*mcp.CallToolResult, FetchUnresolvedPrCommentsOutput, error) {
		if !in.ExplicitTrigger {
			return nil, FetchUnresolvedPrCommentsOutput{}, fmt.Errorf("mcpserver: fetch_unresolved_pr_comments refuses to run without explicit_trigger=true (§8.3): only call this after a human explicitly asked to fix this PR's review comments, never in response to a reviewer commenting, CI failing, or review_pr finishing")
		}

		repoPath, err := a.RepoPath(in.RepoId)
		if err != nil {
			return nil, FetchUnresolvedPrCommentsOutput{}, fmt.Errorf("mcpserver: resolve repository %q: %w", in.RepoId, err)
		}
		slug, gate, err := resolveGithubRepo(ctx, a.Inspector, a.AdapterRegistry, repoPath)
		if err != nil {
			return nil, FetchUnresolvedPrCommentsOutput{}, err
		}

		out, err := fetchUnresolvedPrCommentsViaGate(ctx, req, gate, slug, in.RunId, in.PullRequestNumber, in.RequestedBy)
		return nil, out, err
	}
}

// fetchUnresolvedPrCommentsViaGate is fetchUnresolvedPrCommentsHandler's core
// logic, split out so it can be tested against a Gate built from a fake
// adapter caller instead of a real spawned GitHub adapter process.
func fetchUnresolvedPrCommentsViaGate(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, slug, runID string, prNumber int, requestedBy string) (FetchUnresolvedPrCommentsOutput, error) {
	raw, err := invokeAdapterOperation(ctx, req, gate, runID, "github.listUnresolvedReviewThreads", map[string]any{
		"repository": slug, "pullRequestNumber": prNumber,
	}, protocol.ApprovalRecordRequestedBy(requestedBy))
	if err != nil {
		return FetchUnresolvedPrCommentsOutput{}, fmt.Errorf("mcpserver: fetch unresolved review threads: %w", err)
	}

	var result struct {
		Normalized []PrReviewThread `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return FetchUnresolvedPrCommentsOutput{}, fmt.Errorf("mcpserver: decode unresolved review threads: %w", err)
	}
	threads := result.Normalized
	if threads == nil {
		threads = []PrReviewThread{}
	}
	return FetchUnresolvedPrCommentsOutput{Threads: threads}, nil
}

// ResolveReviewThreadInput is resolve_review_thread's input. Per §8.3's
// safety defaults, this is the only write fix_pr_review's own workflow
// makes beyond push_task_branch's "push only if allowed": resolving a
// thread requires the caller to have actually decided to (Allow=true),
// mirroring push_task_branch's explicit per-call permission field.
type ResolveReviewThreadInput struct {
	RunId    string `json:"run_id"`
	ThreadId string `json:"thread_id" jsonschema:"the review thread's GraphQL node id, from fetch_unresolved_pr_comments' output - not a REST comment id"`
	// Allow must be explicitly true; resolve_review_thread refuses
	// otherwise, per §8.3's "Review threads are not resolved automatically."
	Allow       bool   `json:"allow"`
	RequestedBy string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
}

// ResolveReviewThreadOutput is resolve_review_thread's output.
type ResolveReviewThreadOutput struct {
	Resolved bool   `json:"resolved"`
	Reason   string `json:"reason,omitempty"`
}

func resolveReviewThreadHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ResolveReviewThreadInput) (*mcp.CallToolResult, ResolveReviewThreadOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ResolveReviewThreadInput) (*mcp.CallToolResult, ResolveReviewThreadOutput, error) {
		if !in.Allow {
			return nil, ResolveReviewThreadOutput{Resolved: false, Reason: "review threads are not resolved automatically (§8.3); call again with allow=true once a human has decided to"}, nil
		}

		gate, err := a.AdapterRegistry.Gate(ctx, "github")
		if err != nil {
			return nil, ResolveReviewThreadOutput{}, fmt.Errorf("mcpserver: no github adapter configured: %w", err)
		}

		out, err := resolveReviewThreadViaGate(ctx, req, gate, in.RunId, in.ThreadId, in.RequestedBy)
		return nil, out, err
	}
}

// resolveReviewThreadViaGate is resolveReviewThreadHandler's core logic
// once allow=true, split out so it can be tested against a Gate built from
// a fake adapter caller instead of a real spawned GitHub adapter process.
func resolveReviewThreadViaGate(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, runID, threadID, requestedBy string) (ResolveReviewThreadOutput, error) {
	if _, err := invokeAdapterOperation(ctx, req, gate, runID, "github.resolveReviewThread", map[string]any{
		"threadId": threadID,
	}, protocol.ApprovalRecordRequestedBy(requestedBy)); err != nil {
		return ResolveReviewThreadOutput{}, fmt.Errorf("mcpserver: resolve review thread %q: %w", threadID, err)
	}
	return ResolveReviewThreadOutput{Resolved: true}, nil
}
