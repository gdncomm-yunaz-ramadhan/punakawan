package sources

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ApprovalSource implements contract.ApprovalReader over *app.App's
// approvals.Store. Read-only, per the panel's read-only MVP (§14.8): no
// Approve/Deny method exists here.
type ApprovalSource struct {
	App *app.App
}

func (a *ApprovalSource) List(ctx context.Context, workspaceID string, filter contract.ApprovalFilter) ([]protocol.ApprovalRecord, error) {
	if workspaceID != a.App.Workspace.ID {
		return nil, fmt.Errorf("sources: workspace %q is not available (only %q is)", workspaceID, a.App.Workspace.ID)
	}

	recs, err := a.App.Approvals.List()
	if err != nil {
		return nil, fmt.Errorf("sources: list approvals: %w", err)
	}
	if filter.Status == "" {
		return recs, nil
	}

	var out []protocol.ApprovalRecord
	for _, rec := range recs {
		if string(rec.Status) == filter.Status {
			out = append(out, rec)
		}
	}
	return out, nil
}
