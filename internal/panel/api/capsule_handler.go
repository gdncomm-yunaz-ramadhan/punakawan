package api

import (
	"net/http"

	"github.com/ygrip/punakawan/internal/capsule"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CapsulesHandler serves
// GET /api/v1/workspaces/{workspaceId}/capsules?task_id=<id>, per §14.4's
// "context capsules" metadata (capsule id, role, digest, objective,
// knowledge/evidence reference counts, allowed tools, forbidden actions -
// never the hidden reasoning that produced them). This is keyed by
// task_id, not by session: ContextCapsule has no run/session field, so
// there is no per-session filter to apply yet - a caller wanting "this
// session's capsules" would need task-to-run linkage that doesn't exist
// in the current data model. store is used directly rather than through
// internal/panel/contract, since this is a plain pass-through with no
// format-specific parsing to abstract over.
func CapsulesHandler(store *capsule.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		all, err := store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		taskID := r.URL.Query().Get("task_id")
		items := make([]protocol.ContextCapsule, 0, len(all))
		for _, c := range all {
			if taskID == "" || c.TaskId == taskID {
				items = append(items, c)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
