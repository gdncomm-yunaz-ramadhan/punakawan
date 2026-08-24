package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledgefacade"
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

	// ProjectId is ADR-0020's hub project filter - which project's knowledge
	// store to search, distinct from the record-level "project" scope field
	// above (a ranking bonus on individual records' own provenance, not a
	// database selector). Always pass your own calling project's id
	// explicitly; omitting it also defaults to it, so cross-project access
	// only happens when a caller deliberately names a different project.
	// Naming a project other than your own falls back to a plain substring
	// scan of that project's records (no ranked BM25 index spans projects),
	// and only works when that project shares this one's hub.
	ProjectId string `json:"project_id,omitempty" jsonschema:"which project's knowledge store to search (ADR-0020) - defaults to the calling project; name another project's id to deliberately search it via a lower-fidelity cross-project scan"`

	// Source picks which knowledge provider(s) to search. Omitting it (the
	// zero value) is identical to "local" - both search only
	// Punakawan's own knowledge store, exactly as search_knowledge behaved
	// before this field existed. "all" fans out to every registered
	// provider (local, mom, codepedia) and merges their results, tagging
	// each with its own source/id rather than normalizing them away; a
	// provider with no working backend reports its failure in
	// provider_errors instead of aborting the whole search. Mom and
	// Codepedia are currently unconfigured stubs (no deployment exists yet
	// to point at) - searching one directly returns an error, not empty
	// results.
	Source string `json:"source,omitempty" jsonschema:"which knowledge source to search: local (default, same as omitting this field), mom, codepedia, or all to fan out across every registered provider"`
}

// SearchKnowledgeMatch is §11.12's KnowledgeSearchResult.match.
type SearchKnowledgeMatch struct {
	Kind   string   `json:"kind"`
	Fields []string `json:"fields,omitempty"`
	Terms  []string `json:"terms,omitempty"`
}

// SearchKnowledgeResult is one search_knowledge hit, with an Explanation
// ("Matched because: ...") and a Source naming which provider produced it
// ("local" for every result before federated search existed).
type SearchKnowledgeResult struct {
	Id      string  `json:"id"`
	Title   string  `json:"title"`
	Summary string  `json:"summary,omitempty"`
	Type    string  `json:"type"`
	Score   float64 `json:"score"`
	Source  string  `json:"source"`

	Match       SearchKnowledgeMatch `json:"match"`
	Explanation []string             `json:"explanation"`
}

// SearchKnowledgeProviderError reports one provider's failure during a
// source=all fan-out without aborting the providers that did succeed - the
// chosen policy for a still-unconfigured Mom/Codepedia stub
// (knowledgefacade.ErrProviderNotConfigured), so one missing backend never
// hides results the local store could still answer.
type SearchKnowledgeProviderError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

// SearchKnowledgeOutput is knowledge.search's output.
type SearchKnowledgeOutput struct {
	Results        []SearchKnowledgeResult        `json:"results"`
	ProviderErrors []SearchKnowledgeProviderError `json:"provider_errors,omitempty"`
}

// newLocalKnowledgeProvider builds the local compatibility provider scoped
// to this workspace, reusing App.OpenKnowledge/OpenSearchIndex/SearchKnowledge
// rather than reimplementing BM25 ranking or the locking around its
// rebuild. Opening the search index is deferred to the
// RankedSearch closure so a caller that only ever hits the cross-project
// scan branch (search.Request.ProjectId naming a sibling) never pays for it,
// matching the pre-facade handler's own laziness.
func newLocalKnowledgeProvider(a *app.App) (*knowledgefacade.LegacyLocalKnowledgeProvider, error) {
	store, err := a.OpenKnowledge()
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open knowledge store: %w", err)
	}
	return &knowledgefacade.LegacyLocalKnowledgeProvider{
		Store:   store,
		Project: a.Workspace.ID,
		RankedSearch: func(r search.Request) ([]search.Result, error) {
			ix, err := a.OpenSearchIndex()
			if err != nil {
				return nil, fmt.Errorf("mcpserver: open search index: %w", err)
			}
			return a.SearchKnowledge(store, ix, r)
		},
	}, nil
}

// toSearchKnowledgeResults maps facade results into the tool's own output
// shape, preserving every field a pre-facade local result carried (Type
// comes from the hydrated Record rather than a separate facade field, since
// a provider result doesn't require every provider to know a record's
// type).
func toSearchKnowledgeResults(results []knowledgefacade.ProviderResult) []SearchKnowledgeResult {
	out := make([]SearchKnowledgeResult, len(results))
	for i, r := range results {
		var typ string
		if r.Record != nil {
			typ = string(r.Record.Type)
		}
		out[i] = SearchKnowledgeResult{
			Id:      r.Ref,
			Title:   r.Title,
			Summary: r.Summary,
			Type:    typ,
			Score:   r.Score,
			Source:  r.Source,
			Match: SearchKnowledgeMatch{
				Kind:   r.Match.Kind,
				Fields: r.Match.Fields,
				Terms:  r.Match.Terms,
			},
			Explanation: r.Explanation,
		}
	}
	return out
}

// namedKnowledgeProvider pairs a provider with the source name it is
// registered under, so a source=all fan-out can label a failure without
// relying on a stub's error text.
type namedKnowledgeProvider struct {
	name     string
	provider knowledgefacade.KnowledgeProvider
}

func searchKnowledgeHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SearchKnowledgeInput) (*mcp.CallToolResult, SearchKnowledgeOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SearchKnowledgeInput) (*mcp.CallToolResult, SearchKnowledgeOutput, error) {
		facadeReq := knowledgefacade.SearchRequest{
			Query:          in.Query,
			Project:        in.Project,
			Repository:     in.Repository,
			Module:         in.Module,
			Path:           in.Path,
			Types:          in.Types,
			Tags:           in.Tags,
			IncludeRelated: in.IncludeRelated,
			Limit:          in.Limit,
			ProjectId:      in.ProjectId,
		}

		switch in.Source {
		case "", knowledgefacade.SourceLocal:
			local, err := newLocalKnowledgeProvider(a)
			if err != nil {
				return nil, SearchKnowledgeOutput{}, err
			}
			results, err := local.Search(ctx, facadeReq)
			if err != nil {
				return nil, SearchKnowledgeOutput{}, fmt.Errorf("mcpserver: search knowledge: %w", err)
			}
			return nil, SearchKnowledgeOutput{Results: toSearchKnowledgeResults(results)}, nil

		case knowledgefacade.SourceMom:
			results, err := (knowledgefacade.MomProvider{}).Search(ctx, facadeReq)
			if err != nil {
				return nil, SearchKnowledgeOutput{}, fmt.Errorf("mcpserver: search knowledge (mom): %w", err)
			}
			return nil, SearchKnowledgeOutput{Results: toSearchKnowledgeResults(results)}, nil

		case knowledgefacade.SourceCodepedia:
			results, err := (knowledgefacade.CodepediaProvider{}).Search(ctx, facadeReq)
			if err != nil {
				return nil, SearchKnowledgeOutput{}, fmt.Errorf("mcpserver: search knowledge (codepedia): %w", err)
			}
			return nil, SearchKnowledgeOutput{Results: toSearchKnowledgeResults(results)}, nil

		case "all":
			local, err := newLocalKnowledgeProvider(a)
			if err != nil {
				return nil, SearchKnowledgeOutput{}, err
			}
			providers := []namedKnowledgeProvider{
				{knowledgefacade.SourceLocal, local},
				{knowledgefacade.SourceMom, knowledgefacade.MomProvider{}},
				{knowledgefacade.SourceCodepedia, knowledgefacade.CodepediaProvider{}},
			}
			var out SearchKnowledgeOutput
			for _, np := range providers {
				results, err := np.provider.Search(ctx, facadeReq)
				if err != nil {
					// A stub provider's failure is reported, not fatal: the
					// fan-out still returns whatever the other providers
					// found.
					out.ProviderErrors = append(out.ProviderErrors, SearchKnowledgeProviderError{Source: np.name, Error: err.Error()})
					continue
				}
				out.Results = append(out.Results, toSearchKnowledgeResults(results)...)
			}
			return nil, out, nil

		default:
			return nil, SearchKnowledgeOutput{}, fmt.Errorf("mcpserver: search knowledge: unknown source %q (want local, mom, codepedia, or all)", in.Source)
		}
	}
}
