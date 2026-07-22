package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/diffcheck"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CheckDiffInput is check_diff's input.
type CheckDiffInput struct {
	RunId  string `json:"run_id"`
	TaskId string `json:"task_id"`
	RepoId string `json:"repo_id"`
}

// CheckDiffOutput is check_diff's output.
type CheckDiffOutput struct {
	Allowed      bool     `json:"allowed"`
	ChangedFiles []string `json:"changed_files"`
	Violations   []string `json:"violations,omitempty"`
}

func checkDiffHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CheckDiffInput) (*mcp.CallToolResult, CheckDiffOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CheckDiffInput) (*mcp.CallToolResult, CheckDiffOutput, error) {
		worktreePath := gitops.WorktreePath(a.Workspace.Root, in.RepoId, in.TaskId)

		bundle, err := newEvidenceBundle(a, in.RunId, in.TaskId)
		if err != nil {
			return nil, CheckDiffOutput{}, err
		}

		report, err := diffcheck.Check(ctx, a.Supervisor, worktreePath, a.Policy, bundle)
		if err != nil {
			return nil, CheckDiffOutput{}, fmt.Errorf("mcpserver: check_diff: %w", err)
		}

		ledger, err := newEvidenceLedger(a, in.RunId)
		if err != nil {
			return nil, CheckDiffOutput{}, err
		}
		if _, err := evidence.RecordArtifact(ledger, in.RunId, in.TaskId, protocol.EvidenceRecordTypeGitDiff, bundle, "diff.patch", time.Now().UTC()); err != nil {
			return nil, CheckDiffOutput{}, fmt.Errorf("mcpserver: record diff.patch evidence: %w", err)
		}

		return nil, CheckDiffOutput{
			Allowed:      report.Allowed,
			ChangedFiles: report.ChangedFiles,
			Violations:   report.Violations,
		}, nil
	}
}
