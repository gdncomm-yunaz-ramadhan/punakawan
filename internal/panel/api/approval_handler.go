package api

import (
	"fmt"
	"net/http"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// approvalItem wraps a protocol.ApprovalRecord with the CLI commands that
// resolve it, per §14.8's "CLI resolution hints for pending approvals":
// the panel is read-only (no Approve/Deny endpoint), so the concrete next
// step it can offer is the exact `punakawan approvals` invocation a human
// would run in a terminal. Only set for still-pending records - an
// already-resolved approval has nothing left to resolve.
type approvalItem struct {
	protocol.ApprovalRecord
	ApproveCommand *string `json:"approve_command,omitempty"`
	DenyCommand    *string `json:"deny_command,omitempty"`
}

// ApprovalsHandler serves
// GET /api/v1/workspaces/{workspaceId}/approvals, optionally filtered by
// ?status=pending|approved|denied.
func ApprovalsHandler(reader contract.ApprovalReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := contract.ApprovalFilter{Status: r.URL.Query().Get("status")}
		recs, err := reader.List(r.Context(), r.PathValue("workspaceId"), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		items := make([]approvalItem, 0, len(recs))
		for _, rec := range recs {
			item := approvalItem{ApprovalRecord: rec}
			if rec.Status == protocol.ApprovalRecordStatusPending {
				approve := fmt.Sprintf("punakawan approvals approve %s --by <your-name>", rec.Id)
				deny := fmt.Sprintf("punakawan approvals deny %s --by <your-name>", rec.Id)
				item.ApproveCommand = &approve
				item.DenyCommand = &deny
			}
			items = append(items, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
