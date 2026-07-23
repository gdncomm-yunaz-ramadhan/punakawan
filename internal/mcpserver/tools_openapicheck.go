package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/fileops"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/openapicheck"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CheckOpenAPICompatibilityInput is check_openapi_compatibility's input.
// BasePath/HeadPath are resolved within this task's worktree, not treated as
// unconfined absolute host paths (punokawan-doe): they must be relative to the
// worktree root and cannot escape it via "..", the same confinement the
// file-write tools apply via internal/fileops.
type CheckOpenAPICompatibilityInput struct {
	RunId    string `json:"run_id"`
	TaskId   string `json:"task_id"`
	RepoId   string `json:"repo_id" jsonschema:"repository id as declared in the workspace; base_path/head_path are resolved within this task's worktree for that repo"`
	BasePath string `json:"base_path" jsonschema:"path to the base (pre-change) OpenAPI spec, relative to the task worktree root"`
	HeadPath string `json:"head_path" jsonschema:"path to the head (post-change) OpenAPI spec, relative to the task worktree root"`
}

func checkOpenAPICompatibilityHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CheckOpenAPICompatibilityInput) (*mcp.CallToolResult, openapicheck.Result, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CheckOpenAPICompatibilityInput) (*mcp.CallToolResult, openapicheck.Result, error) {
		worktreeRoot := gitops.WorktreePath(a.Workspace.Root, in.RepoId, in.TaskId)
		basePath, err := fileops.ResolveWithinRoot(worktreeRoot, in.BasePath)
		if err != nil {
			return nil, openapicheck.Result{}, fmt.Errorf("mcpserver: check_openapi_compatibility: base_path: %w", err)
		}
		headPath, err := fileops.ResolveWithinRoot(worktreeRoot, in.HeadPath)
		if err != nil {
			return nil, openapicheck.Result{}, fmt.Errorf("mcpserver: check_openapi_compatibility: head_path: %w", err)
		}

		result, err := openapicheck.Check(basePath, headPath)
		if err != nil {
			return nil, openapicheck.Result{}, fmt.Errorf("mcpserver: check_openapi_compatibility: %w", err)
		}

		bundle, err := newEvidenceBundle(a, in.RunId, in.TaskId)
		if err != nil {
			return nil, openapicheck.Result{}, err
		}
		if err := openapicheck.WriteEvidence(bundle, result); err != nil {
			return nil, openapicheck.Result{}, fmt.Errorf("mcpserver: write api-diff.json: %w", err)
		}

		ledger, err := newEvidenceLedger(a, in.RunId)
		if err != nil {
			return nil, openapicheck.Result{}, err
		}
		if _, err := evidence.RecordArtifact(ledger, in.RunId, in.TaskId, protocol.EvidenceRecordTypeApiDiff, bundle, "api-diff.json", time.Now().UTC()); err != nil {
			return nil, openapicheck.Result{}, fmt.Errorf("mcpserver: record api-diff.json evidence: %w", err)
		}

		return nil, result, nil
	}
}
