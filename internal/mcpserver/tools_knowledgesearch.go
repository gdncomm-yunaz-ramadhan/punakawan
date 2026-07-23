package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/search"
)

// SearchKnowledgeInput is knowledge.search's input, per
// punakawan-architecture-enhancement-plan.md §11.12/§12.1. Scope fields are
// a ranking signal (§11.10's scope bonus), not a filter - a result outside
// them still comes back, just ranked lower. Types/Tags, by contrast, are
// hard filters.
type SearchKnowledgeInput struct {
	Query string `json:"query"`

	Project    string `json:"project,omitempty"`
	Repository string `json:"repository,omitempty"`
	Module     string `json:"module,omitempty"`
	Path       string `json:"path,omitempty"`

	Types []string `json:"types,omitempty"`
	Tags  []string `json:"tags,omitempty"`

	IncludeRelated bool `json:"include_related,omitempty" jsonschema:"expand one hop of directly related records (§11.9), bounded to 10 items"`
	Limit          int  `json:"limit,omitempty" jsonschema:"maximum results to return, default 20"`
}

// SearchKnowledgeMatch is §11.12's KnowledgeSearchResult.match.
type SearchKnowledgeMatch struct {
	Kind   string   `json:"kind"`
	Fields []string `json:"fields,omitempty"`
	Terms  []string `json:"terms,omitempty"`
}

// SearchKnowledgeResult is §11.12's KnowledgeSearchResult, plus Explanation
// for §11.13's search explanation ("Matched because: ...").
type SearchKnowledgeResult struct {
	Id      string  `json:"id"`
	Title   string  `json:"title"`
	Summary string  `json:"summary,omitempty"`
	Type    string  `json:"type"`
	Score   float64 `json:"score"`

	Match       SearchKnowledgeMatch `json:"match"`
	Explanation []string             `json:"explanation"`
}

// SearchKnowledgeOutput is knowledge.search's output.
type SearchKnowledgeOutput struct {
	Results []SearchKnowledgeResult `json:"results"`
}

func searchKnowledgeHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SearchKnowledgeInput) (*mcp.CallToolResult, SearchKnowledgeOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SearchKnowledgeInput) (*mcp.CallToolResult, SearchKnowledgeOutput, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, SearchKnowledgeOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}
		ix, err := a.OpenSearchIndex()
		if err != nil {
			return nil, SearchKnowledgeOutput{}, fmt.Errorf("mcpserver: open search index: %w", err)
		}

		// §11.11's index is disposable and always rebuildable from the store.
		// App.SearchKnowledge re-syncs it before querying so a deleted or
		// changed record can never surface, but that rebuild is watermark-gated
		// (a no-op when nothing changed since the last sync, punokawan-77q) and
		// serialized with the read under one lock so two concurrent searches
		// cannot race the shared index (punokawan-hzp).
		results, err := a.SearchKnowledge(store, ix, search.Request{
			Query: in.Query,
			Scope: search.Scope{
				Project:    in.Project,
				Repository: in.Repository,
				Module:     in.Module,
				Path:       in.Path,
			},
			Types:          in.Types,
			Tags:           in.Tags,
			IncludeRelated: in.IncludeRelated,
			Limit:          in.Limit,
		})
		if err != nil {
			return nil, SearchKnowledgeOutput{}, fmt.Errorf("mcpserver: search knowledge: %w", err)
		}

		out := make([]SearchKnowledgeResult, len(results))
		for i, r := range results {
			out[i] = SearchKnowledgeResult{
				Id:      r.Id,
				Title:   r.Title,
				Summary: r.Summary,
				Type:    r.Type,
				Score:   r.Score,
				Match: SearchKnowledgeMatch{
					Kind:   string(r.Match.Kind),
					Fields: r.Match.Fields,
					Terms:  r.Match.Terms,
				},
				Explanation: r.Explanation,
			}
		}
		return nil, SearchKnowledgeOutput{Results: out}, nil
	}
}
