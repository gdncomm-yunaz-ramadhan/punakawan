package sources

import (
	"context"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/search"
)

// GlobalSearchSource implements contract.GlobalSearchReader by querying
// every registered workspace's own knowledge store and BM25F index in
// turn, then fusing the per-workspace ranked lists with
// search.FuseRankedLists, per §10.1.
//
// The workspace App itself was loaded for is always included, whether or
// not it happens to also be a registry entry: a caller running the panel
// against a workspace it never separately registered still expects
// "global search" to cover the workspace it is looking at.
//
// A workspace that fails to load or search (missing path, corrupt store)
// is skipped rather than failing the whole global search, matching Phase
// 2's "one broken workspace does not break the page" precedent - the same
// reason WorkspaceSource degrades a broken entry instead of erroring.
type GlobalSearchSource struct {
	App      *app.App
	Registry *registry.Store
}

func (g *GlobalSearchSource) Search(ctx context.Context, req search.Request) ([]contract.GlobalSearchResult, error) {
	entries := []struct {
		id   string
		path string
	}{{g.App.Workspace.ID, g.App.Workspace.Root}}
	seen := map[string]bool{g.App.Workspace.ID: true}

	if g.Registry != nil {
		regEntries, err := g.Registry.List()
		if err != nil {
			return nil, err
		}
		for _, e := range regEntries {
			if seen[e.Id] {
				continue
			}
			seen[e.Id] = true
			entries = append(entries, struct{ id, path string }{e.Id, e.Path})
		}
	}

	lists := make([]search.RankedList, 0, len(entries))
	for _, e := range entries {
		var results []search.Result
		if e.id == g.App.Workspace.ID {
			store, err := g.App.OpenKnowledge()
			if err != nil {
				continue
			}
			ix, err := g.App.OpenSearchIndex()
			if err != nil {
				continue
			}
			results, err = search.Search(store, ix, req)
			if err != nil {
				continue
			}
		} else {
			other, err := app.Load(e.path)
			if err != nil {
				continue
			}
			store, err := other.OpenKnowledge()
			if err == nil {
				var ix *search.Index
				ix, err = other.OpenSearchIndex()
				if err == nil {
					results, _ = search.Search(store, ix, req)
				}
			}
			other.Close()
		}
		if len(results) > 0 {
			lists = append(lists, search.RankedList{WorkspaceID: e.id, Results: results})
		}
	}

	fused := search.FuseRankedLists(lists)
	out := make([]contract.GlobalSearchResult, 0, len(fused))
	for _, f := range fused {
		out = append(out, contract.GlobalSearchResult{WorkspaceID: f.WorkspaceID, Result: f.Result, RRFScore: f.RRFScore})
	}
	return out, nil
}
