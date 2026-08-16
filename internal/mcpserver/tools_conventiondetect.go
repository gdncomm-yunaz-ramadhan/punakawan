package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/convention"
)

// DetectNoTernaryConventionInput is detect_no_ternary_convention's input.
// RepoId names a declared workspace repository (workspace.Workspace.Repositories);
// left empty, the scan runs over the whole workspace root, which is
// equivalent to RepoId for a single-repository workspace.
type DetectNoTernaryConventionInput struct {
	RepoId string `json:"repo_id,omitempty" jsonschema:"declared repository id to scan; defaults to the whole workspace root"`
}

// DetectNoTernaryConventionOutput reports whether the heuristic (see
// internal/convention.DetectNoTernaryConvention) found enough evidence to
// open or reinforce a pending convention proposal, and if so, its identity.
type DetectNoTernaryConventionOutput struct {
	Found        bool   `json:"found"`
	ProposalId   string `json:"proposal_id,omitempty"`
	Fingerprint  string `json:"fingerprint,omitempty"`
	SupportCount int    `json:"support_count,omitempty"`
	Status       string `json:"status,omitempty"`
}

// detectNoTernaryConventionHandler runs internal/convention's honestly-scoped
// ternary-emulation-helper heuristic over one repository and records any
// resulting candidate as a pending, inferred learning proposal. This is
// intentionally a manually-triggered tool, not something wired onto every
// request or preflight: detection over a whole repository tree is not free, and this
// vertical slice's scope is proving the dormant-proposal-to-approved-
// visibility pipeline works for one concrete example, not building an
// always-on convention scanner.
func detectNoTernaryConventionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, DetectNoTernaryConventionInput) (*mcp.CallToolResult, DetectNoTernaryConventionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in DetectNoTernaryConventionInput) (*mcp.CallToolResult, DetectNoTernaryConventionOutput, error) {
		repoPath := a.Workspace.Root
		if in.RepoId != "" {
			p, err := a.Workspace.RepositoryPath(in.RepoId)
			if err != nil {
				return nil, DetectNoTernaryConventionOutput{}, fmt.Errorf("detect_no_ternary_convention: %w", err)
			}
			repoPath = p
		}

		store, err := a.OpenLearning()
		if err != nil {
			return nil, DetectNoTernaryConventionOutput{}, fmt.Errorf("detect_no_ternary_convention: open learning store: %w", err)
		}

		proposal, found, err := convention.RecordNoTernaryConvention(store, repoPath, a.Workspace.ID)
		if err != nil {
			return nil, DetectNoTernaryConventionOutput{}, fmt.Errorf("detect_no_ternary_convention: %w", err)
		}
		if !found {
			return nil, DetectNoTernaryConventionOutput{Found: false}, nil
		}
		return nil, DetectNoTernaryConventionOutput{
			Found:        true,
			ProposalId:   proposal.Id,
			Fingerprint:  proposal.Fingerprint,
			SupportCount: proposal.SupportCount,
			Status:       proposal.Status,
		}, nil
	}
}
