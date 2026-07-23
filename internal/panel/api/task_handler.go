package api

import (
	"net/http"
	"strconv"

	"github.com/ygrip/punakawan/internal/panel/contract"
)

// TasksHandler serves GET /api/v1/workspaces/{workspaceId}/tasks, per
// §11.4. Supported filters: status, priority, type, assignee,
// external_issue, query, limit, and blocked (an alias for status=blocked,
// matching the plan's documented filter name - see TaskSource.List for why
// this only catches issues bd's own readiness computation, not just its
// stored status, considers blocked). role and repository_id are parsed
// but not applied: bd's issue schema has no per-task role or repository
// field to filter on.
func TasksHandler(reader contract.TaskReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := contract.TaskFilter{
			Status:        q.Get("status"),
			Priority:      q.Get("priority"),
			Type:          q.Get("type"),
			Assignee:      q.Get("assignee"),
			ExternalIssue: q.Get("external_issue"),
			Query:         q.Get("query"),
		}
		if limit, err := strconv.Atoi(q.Get("limit")); err == nil {
			filter.Limit = limit
		}

		issues, err := reader.List(r.Context(), r.PathValue("workspaceId"), filter)
		if err != nil {
			writeError(w, listErrorStatus(err), err)
			return
		}
		// blocked=true filters by BoardStatus rather than passing
		// filter.Status="blocked" through: bd's stored status alone
		// under-counts blocked issues (see TaskSource.List), so this must
		// run after BoardStatus has been computed.
		if blocked, err := strconv.ParseBool(q.Get("blocked")); err == nil && blocked {
			filtered := make([]contract.TaskSummary, 0, len(issues))
			for _, issue := range issues {
				if issue.BoardStatus == "blocked" {
					filtered = append(filtered, issue)
				}
			}
			issues = filtered
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": issues})
	}
}

// TaskHandler serves GET /api/v1/workspaces/{workspaceId}/tasks/{taskId}.
func TaskHandler(reader contract.TaskReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		issue, err := reader.Get(r.Context(), r.PathValue("workspaceId"), r.PathValue("taskId"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, issue)
	}
}

// TaskGraphHandler serves GET /api/v1/workspaces/{workspaceId}/task-graph,
// the full dependency graph behind §14.5's dependency view.
func TaskGraphHandler(reader contract.TaskReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		graph, err := reader.Dependencies(r.Context(), r.PathValue("workspaceId"))
		if err != nil {
			writeError(w, listErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, graph)
	}
}
