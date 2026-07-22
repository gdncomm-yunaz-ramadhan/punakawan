package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/openapicheck"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CheckOpenAPICompatibilityInput is check_openapi_compatibility's input.
type CheckOpenAPICompatibilityInput struct {
	RunId    string `json:"run_id"`
	TaskId   string `json:"task_id"`
	BasePath string `json:"base_path" jsonschema:"path to the base (pre-change) OpenAPI spec file"`
	HeadPath string `json:"head_path" jsonschema:"path to the head (post-change) OpenAPI spec file"`
}

func checkOpenAPICompatibilityHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CheckOpenAPICompatibilityInput) (*mcp.CallToolResult, openapicheck.Result, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CheckOpenAPICompatibilityInput) (*mcp.CallToolResult, openapicheck.Result, error) {
		result, err := openapicheck.Check(in.BasePath, in.HeadPath)
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
