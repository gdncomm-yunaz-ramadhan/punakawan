package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ListTaskEvidenceInput is list_task_evidence's input.
type ListTaskEvidenceInput struct {
	RunId  string `json:"run_id"`
	TaskId string `json:"task_id"`
}

// ListTaskEvidenceOutput is list_task_evidence's output.
type ListTaskEvidenceOutput struct {
	Records []protocol.EvidenceRecord `json:"records"`
}

// listTaskEvidenceHandler returns every EvidenceRecord check_diff, run_tests,
// and check_openapi_compatibility have recorded for run_id/task_id
// (punokawan-s12), so a reviewer (Bagong, Semar) can enumerate a task's
// evidence structurally instead of having to know the bundle's file-naming
// convention.
func listTaskEvidenceHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListTaskEvidenceInput) (*mcp.CallToolResult, ListTaskEvidenceOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListTaskEvidenceInput) (*mcp.CallToolResult, ListTaskEvidenceOutput, error) {
		ledger, err := newEvidenceLedger(a, in.RunId)
		if err != nil {
			return nil, ListTaskEvidenceOutput{}, err
		}
		records, err := ledger.ForTask(in.TaskId)
		if err != nil {
			return nil, ListTaskEvidenceOutput{}, fmt.Errorf("mcpserver: list task evidence: %w", err)
		}
		if records == nil {
			records = []protocol.EvidenceRecord{}
		}
		return nil, ListTaskEvidenceOutput{Records: records}, nil
	}
}
