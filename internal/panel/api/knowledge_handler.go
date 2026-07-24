package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// parseSearchRequest builds a search.Request from query parameters. §10.1
// describes an inline colon-syntax ("type:requirement state:verified
// checkout refund") for filters; this instead uses separate query
// parameters (type, repo, limit) plus a free-text q, matching every other
// panel list endpoint's convention (status=, priority=, ... on
// tasks/sessions) rather than adding a bespoke text parser.
func parseSearchRequest(q map[string][]string) search.Request {
	get := func(key string) string {
		if v, ok := q[key]; ok && len(v) > 0 {
			return v[0]
		}
		return ""
	}
	req := search.Request{
		Query: get("q"),
		Scope: search.Scope{Repository: get("repo")},
	}
	if t := get("type"); t != "" {
		req.Types = strings.Split(t, ",")
	}
	if limit, err := strconv.Atoi(get("limit")); err == nil {
		req.Limit = limit
	}
	if include, err := strconv.ParseBool(get("include_related")); err == nil {
		req.IncludeRelated = include
	}
	return req
}

// KnowledgeListHandler serves GET /api/v1/workspaces/{workspaceId}/knowledge,
// per §14.6's filter rail (type, state, repository, source, stale) plus an
// optional q for free-text relevance search - reader.Search when q is
// present (search.Search returns nothing for an empty query, so List
// alone cannot serve a text query), reader.List otherwise.
func KnowledgeListHandler(reader contract.KnowledgeReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		workspaceID := r.PathValue("workspaceId")

		if q := query.Get("q"); q != "" {
			results, err := reader.Search(r.Context(), workspaceID, parseSearchRequest(query))
			if err != nil {
				writeError(w, listErrorStatus(err), err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": results})
			return
		}

		filter := contract.KnowledgeFilter{
			Type:       query.Get("type"),
			State:      query.Get("state"),
			Repository: query.Get("repository"),
			Source:     query.Get("source"),
		}
		if stale, err := strconv.ParseBool(query.Get("stale")); err == nil {
			filter.Stale = stale
		}
		if hasRelation, err := strconv.ParseBool(query.Get("has_relation")); err == nil {
			filter.HasRelation = hasRelation
		}
		if hasConflict, err := strconv.ParseBool(query.Get("has_conflict")); err == nil {
			filter.HasConflict = hasConflict
		}
		if limit, err := strconv.Atoi(query.Get("limit")); err == nil {
			filter.Limit = limit
		}

		records, err := reader.List(r.Context(), workspaceID, filter)
		if err != nil {
			writeError(w, listErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": records})
	}
}

// KnowledgeHandler serves
// GET /api/v1/workspaces/{workspaceId}/knowledge/{knowledgeId}.
func KnowledgeHandler(reader contract.KnowledgeReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec, err := reader.Get(r.Context(), r.PathValue("workspaceId"), r.PathValue("knowledgeId"))
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

// KnowledgeRelationsHandler serves
// GET /api/v1/workspaces/{workspaceId}/knowledge/{knowledgeId}/relations.
func KnowledgeRelationsHandler(reader contract.KnowledgeReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		related, err := reader.Relations(r.Context(), r.PathValue("workspaceId"), r.PathValue("knowledgeId"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if related == nil {
			related = []protocol.KnowledgeRecord{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": related})
	}
}

// KnowledgeHistoryHandler serves
// GET /api/v1/workspaces/{workspaceId}/knowledge/{knowledgeId}/history.
func KnowledgeHistoryHandler(reader contract.KnowledgeReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := reader.History(r.Context(), r.PathValue("workspaceId"), r.PathValue("knowledgeId"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": events})
	}
}

// KnowledgeDetailHandler serves everything under
// GET /api/v1/workspaces/{workspaceId}/knowledge/{knowledgeRest...}:
// detail, relations, and history. It cannot be split into three routes
// each with a literal {knowledgeId} segment: knowledge IDs contain literal
// slashes (protocol.KnowledgeRecord.Id's schema pattern is
// "pkw:<type>/<repo-or-workspace>/<name>"), and Go's ServeMux only
// supports a slash-swallowing {name...} wildcard as the final segment of
// a pattern - it cannot be followed by a literal suffix like "/relations"
// in the same route. This handler instead captures the whole remainder
// and peels a known suffix off in Go.
func KnowledgeDetailHandler(reader contract.KnowledgeReader) http.HandlerFunc {
	get := KnowledgeHandler(reader)
	relations := KnowledgeRelationsHandler(reader)
	history := KnowledgeHistoryHandler(reader)
	return func(w http.ResponseWriter, r *http.Request) {
		rest := r.PathValue("knowledgeRest")
		switch {
		case strings.HasSuffix(rest, "/relations"):
			r.SetPathValue("knowledgeId", strings.TrimSuffix(rest, "/relations"))
			relations(w, r)
		case strings.HasSuffix(rest, "/history"):
			r.SetPathValue("knowledgeId", strings.TrimSuffix(rest, "/history"))
			history(w, r)
		default:
			r.SetPathValue("knowledgeId", rest)
			get(w, r)
		}
	}
}

// GlobalSearchHandler serves GET /api/v1/search, per §10.1: every
// registered workspace searched and fused into one ranked list.
func GlobalSearchHandler(reader contract.GlobalSearchReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		results, err := reader.Search(r.Context(), parseSearchRequest(r.URL.Query()))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": results})
	}
}
