package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CreatePrInput is create_pr's input, per
// punakawan-architecture-enhancement-plan.md §8.1's CreatePrInput. The
// narrative body sections (Summary/Requirements/Changes/Verification) are
// required, caller-supplied prose rather than something this tool invents
// - per ADR-0016, punakawan never reasons itself; it only templates
// whatever content the calling agent (which did the actual implementation
// and verification) supplies into the PR body's required section
// structure.
type CreatePrInput struct {
	RunId  string `json:"run_id"`
	RepoId string `json:"repo_id"`

	BaseBranch string `json:"base_branch"`
	HeadBranch string `json:"head_branch"`

	// Title defaults to head_branch if empty.
	Title string `json:"title,omitempty"`
	Draft bool   `json:"draft,omitempty"`

	Summary      string `json:"summary" jsonschema:"required PR body section: what changed and why, in the caller's own words"`
	Requirements string `json:"requirements" jsonschema:"required PR body section: which requirement(s) this satisfies"`
	Changes      string `json:"changes" jsonschema:"required PR body section: what changed, file by file or area by area"`
	Verification string `json:"verification" jsonschema:"required PR body section: what was run to verify this (build/tests/checks) and the result"`
	// SecurityAndQualityChecks/KnownRisks/DeferredWork default to "None."
	// when empty - a caller genuinely may have nothing to report there,
	// and that is itself a valid, honest value, not an invented one.
	SecurityAndQualityChecks string `json:"security_and_quality_checks,omitempty"`
	KnownRisks               string `json:"known_risks,omitempty"`
	DeferredWork             string `json:"deferred_work,omitempty"`

	TaskIds      []string `json:"task_ids" jsonschema:"BD task ids this PR implements; rendered as the PR body's BD task references section"`
	JiraKeys     []string `json:"jira_keys,omitempty" jsonschema:"Jira issue keys this PR relates to; the Jira references section is omitted entirely if empty, per §8.1 ('when available')"`
	KnowledgeIds []string `json:"knowledge_ids,omitempty" jsonschema:"durable knowledge record ids this PR updates or is informed by"`

	Reviewers []string `json:"reviewers,omitempty"`
	Labels    []string `json:"labels,omitempty"`

	RequestedBy string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
}

// CreatePrOutput is create_pr's output. Created=false with Reason set is
// the normal, expected result whenever PR creation is not currently
// possible (§8.1's Failure Behavior: "Punakawan must continue with the
// implementation and report the actual reason" - not an error the caller
// must handle specially).
type CreatePrOutput struct {
	Created  bool   `json:"created"`
	Reason   string `json:"reason,omitempty"`
	PrNumber int    `json:"pr_number,omitempty"`
	PrUrl    string `json:"pr_url,omitempty"`
}

func createPrHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreatePrInput) (*mcp.CallToolResult, CreatePrOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreatePrInput) (*mcp.CallToolResult, CreatePrOutput, error) {
		repoPath, err := a.RepoPath(in.RepoId)
		if err != nil {
			return nil, CreatePrOutput{}, fmt.Errorf("mcpserver: resolve repository %q: %w", in.RepoId, err)
		}

		out, err := createPr(ctx, req, a.Inspector, a.AdapterRegistry, repoPath, in)
		return nil, out, err
	}
}

// createPr is createPrHandler's core logic: detect the repository's real
// git capabilities, then delegate to createPrFromCapabilities.
func createPr(ctx context.Context, req *mcp.CallToolRequest, inspector *gitops.Inspector, registry adapterGateProvider, repoPath string, in CreatePrInput) (CreatePrOutput, error) {
	caps, err := inspector.DetectCapabilities(ctx, repoPath, "origin")
	if err != nil {
		return CreatePrOutput{}, fmt.Errorf("mcpserver: detect git capabilities: %w", err)
	}
	return createPrFromCapabilities(ctx, req, caps, registry, in)
}

// createPrFromCapabilities is createPr's logic once caps is already known,
// split out so it can be tested with a synthetic GitCapabilities (a real
// pushable-to-GitHub remote cannot exist in a test sandbox) and a Gate
// built from a fake adapter caller (mirroring
// internal/mcpserver/tools_jiraprogress.go's updateJiraTaskProgress split)
// instead of a real spawned GitHub adapter process.
func createPrFromCapabilities(ctx context.Context, req *mcp.CallToolRequest, caps protocol.GitCapabilities, registry adapterGateProvider, in CreatePrInput) (CreatePrOutput, error) {
	if reason := unavailableReason(caps); reason != "" {
		return CreatePrOutput{Created: false, Reason: reason}, nil
	}

	var origin *protocol.GitCapabilitiesRemotesElem
	for i := range caps.Remotes {
		if caps.Remotes[i].Name == "origin" {
			origin = &caps.Remotes[i]
			break
		}
	}
	slug, ok := gitops.RepoSlug(origin.FetchUrl)
	if !ok {
		return CreatePrOutput{Created: false, Reason: fmt.Sprintf("unsupported provider: could not determine owner/repo from remote %q", origin.FetchUrl)}, nil
	}

	gate, err := registry.Gate(ctx, "github")
	if err != nil {
		return CreatePrOutput{Created: false, Reason: fmt.Sprintf("authentication unavailable: no github adapter configured: %v", err)}, nil
	}

	title := in.Title
	if title == "" {
		title = in.HeadBranch
	}

	raw, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "github.createPullRequest", map[string]any{
		"repository": slug,
		"baseBranch": in.BaseBranch,
		"headBranch": in.HeadBranch,
		"title":      title,
		"body":       buildPrBody(in),
		"draft":      in.Draft,
	}, protocol.ApprovalRecordRequestedBy(in.RequestedBy))
	if err != nil {
		return CreatePrOutput{Created: false, Reason: err.Error()}, nil
	}

	var result struct {
		Normalized struct {
			Number int    `json:"number"`
			Url    string `json:"url"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return CreatePrOutput{}, fmt.Errorf("mcpserver: decode create_pr response: %w", err)
	}

	if len(in.Labels) > 0 {
		if _, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "github.addLabels", map[string]any{
			"repository": slug, "pullRequestNumber": result.Normalized.Number, "labels": in.Labels,
		}, protocol.ApprovalRecordRequestedBy(in.RequestedBy)); err != nil {
			return CreatePrOutput{}, fmt.Errorf("mcpserver: add labels to PR #%d (PR was created): %w", result.Normalized.Number, err)
		}
	}
	if len(in.Reviewers) > 0 {
		if _, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "github.requestReviewers", map[string]any{
			"repository": slug, "pullRequestNumber": result.Normalized.Number, "reviewers": in.Reviewers,
		}, protocol.ApprovalRecordRequestedBy(in.RequestedBy)); err != nil {
			return CreatePrOutput{}, fmt.Errorf("mcpserver: request reviewers on PR #%d (PR was created): %w", result.Normalized.Number, err)
		}
	}

	return CreatePrOutput{Created: true, PrNumber: result.Normalized.Number, PrUrl: result.Normalized.Url}, nil
}

// adapterGateProvider is the subset of *adapters.Registry's behavior
// createPr depends on, so tests can substitute a Registry pointed at a
// fake/local adapter spec instead of one that spawns real subprocesses.
type adapterGateProvider interface {
	Gate(ctx context.Context, adapterID string) (*adapters.Gate, error)
}

// unavailableReason checks caps against §8.1's preconditions ("It may run
// when: Git is detected. A remote provider is detected. Push access is
// available...") and returns the first one that fails, in the same order
// §8.1 lists them, or "" if create_pr may proceed.
func unavailableReason(caps protocol.GitCapabilities) string {
	switch {
	case !caps.Detected:
		return "no git repository detected"
	case len(caps.Remotes) == 0:
		return "no remote configured"
	case caps.Provider == nil || *caps.Provider != protocol.GitCapabilitiesProviderGithub:
		return "unsupported provider: no github remote detected"
	case !caps.Capabilities.Push:
		return "no push access: " + strings.Join(caps.Limitations, "; ")
	default:
		return ""
	}
}

// buildPrBody templates in's caller-supplied sections into §8.1's required
// PR body structure. It performs no reasoning of its own - every section's
// content is either verbatim caller input or a mechanical listing of ids
// the caller already supplied.
func buildPrBody(in CreatePrInput) string {
	fallback := func(s string) string {
		if s == "" {
			return "None."
		}
		return s
	}
	bulletList := func(items []string) string {
		if len(items) == 0 {
			return "None."
		}
		lines := make([]string, len(items))
		for i, item := range items {
			lines[i] = "- " + item
		}
		return strings.Join(lines, "\n")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## Summary\n\n%s\n\n", in.Summary)
	fmt.Fprintf(&b, "## Requirements\n\n%s\n\n", in.Requirements)
	fmt.Fprintf(&b, "## Changes\n\n%s\n\n", in.Changes)
	fmt.Fprintf(&b, "## Verification\n\n%s\n\n", in.Verification)
	fmt.Fprintf(&b, "## Security and quality checks\n\n%s\n\n", fallback(in.SecurityAndQualityChecks))
	fmt.Fprintf(&b, "## Known risks\n\n%s\n\n", fallback(in.KnownRisks))
	fmt.Fprintf(&b, "## Deferred work\n\n%s\n\n", fallback(in.DeferredWork))
	fmt.Fprintf(&b, "## BD task references\n\n%s\n\n", bulletList(in.TaskIds))
	if len(in.JiraKeys) > 0 {
		fmt.Fprintf(&b, "## Jira references\n\n%s\n\n", bulletList(in.JiraKeys))
	}
	fmt.Fprintf(&b, "## Durable knowledge updates\n\n%s\n", bulletList(in.KnowledgeIds))
	return b.String()
}
