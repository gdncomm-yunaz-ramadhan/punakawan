package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/githubintegration"
	"github.com/ygrip/punakawan/pkg/protocol"
)

type CreateGitHubPullRequestInput struct {
	Repository string `json:"repository,omitempty"`
	BaseBranch string `json:"base_branch"`
	HeadBranch string `json:"head_branch"`
	Title      string `json:"title"`
	Body       string `json:"body,omitempty"`
}

type CreateGitHubPullRequestOutput struct {
	// Status is "created", or "needs_input" when the repository named
	// could not be settled without a decision only a human can make.
	Status     string                  `json:"status,omitempty"`
	NeedsInput *protocol.NeedUserInput `json:"needs_input,omitempty"`
	// Repository is the exact owner/repo this call opened the pull
	// request against, which is not always the string the caller passed:
	// a bare name or an omitted one is resolved here.
	Repository string `json:"repository,omitempty"`
	Number     int    `json:"number,omitempty"`
	URL        string `json:"url,omitempty"`
}

// createGitHubPullRequestHandler opens a pull request through
// githubintegration.Service.CreatePullRequest, attempting it synchronously
// so this tool call returns a definitive result the same way every other
// GitHub write tool does. A retried call for the same repository/head/base
// resolves to the same durable intent instead of opening a second pull
// request - CreatePullRequest's own fingerprinting handles that, nothing
// here needs to.
func createGitHubPullRequestHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreateGitHubPullRequestInput) (*mcp.CallToolResult, CreateGitHubPullRequestOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CreateGitHubPullRequestInput) (*mcp.CallToolResult, CreateGitHubPullRequestOutput, error) {
		baseBranch := strings.TrimSpace(in.BaseBranch)
		headBranch := strings.TrimSpace(in.HeadBranch)
		title := strings.TrimSpace(in.Title)
		var missing []string
		if baseBranch == "" {
			missing = append(missing, "base_branch")
		}
		if headBranch == "" {
			missing = append(missing, "head_branch")
		}
		if title == "" {
			missing = append(missing, "title")
		}
		if len(missing) > 0 {
			return nil, CreateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: create GitHub pull request requires %s", strings.Join(missing, ", "))
		}

		outboxStore, err := a.OpenOutbox()
		if err != nil {
			return nil, CreateGitHubPullRequestOutput{}, err
		}
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, CreateGitHubPullRequestOutput{}, err
		}
		repository, needsInput, err := resolveGitHubRepository(ctx, a, store, in.Repository)
		if err != nil {
			return nil, CreateGitHubPullRequestOutput{}, err
		}
		if needsInput != nil {
			return nil, CreateGitHubPullRequestOutput{Status: "needs_input", NeedsInput: needsInput}, nil
		}
		svc := githubintegration.NewService(a.AdapterRegistry, outboxStore, gitHubOrgResolver(a, store))
		runID := fmt.Sprintf("github-create-pr-%s-%s-%s", repository, headBranch, baseBranch)
		number, url, err := svc.CreatePullRequest(ctx, githubintegration.CreatePullRequestRequest{
			RunID: runID, Repository: repository, BaseBranch: baseBranch, HeadBranch: headBranch, Title: title, Body: in.Body,
		})
		if err != nil {
			return nil, CreateGitHubPullRequestOutput{}, fmt.Errorf("mcpserver: create GitHub pull request: %w", err)
		}
		return nil, CreateGitHubPullRequestOutput{Status: "created", Repository: repository, Number: number, URL: url}, nil
	}
}
