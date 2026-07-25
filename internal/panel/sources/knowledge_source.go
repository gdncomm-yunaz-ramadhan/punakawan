package sources

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// KnowledgeSource implements contract.KnowledgeReader over *app.App's
// knowledge store and BM25F search index (AEP-M6). It reuses
// internal/search.Search directly rather than reimplementing ranking.
type KnowledgeSource struct {
	App *app.App
}

func (k *KnowledgeSource) checkWorkspace(workspaceID string) error {
	if workspaceID != k.App.Workspace.ID {
		return fmt.Errorf("sources: workspace %q is not available (only %q is): %w", workspaceID, k.App.Workspace.ID, contract.ErrWorkspaceUnavailable)
	}
	return nil
}

func hasRelationType(rec protocol.KnowledgeRecord, relType string) bool {
	for _, rel := range rec.Relations {
		if string(rel.Type) == relType {
			return true
		}
	}
	return false
}

// matchesKnowledgePostFilter applies the two filter dimensions that have no
// SQL column and therefore cannot be pushed into knowledge.ListRecords:
// HasRelation and HasConflict are both derived from the record's embedded
// Relations list (see contract.KnowledgeFilter's doc comment). Every other
// dimension is filtered in SQL before decoding, so this only runs when at
// least one of these two flags is set.
func matchesKnowledgePostFilter(rec protocol.KnowledgeRecord, filter contract.KnowledgeFilter) bool {
	if filter.HasRelation && len(rec.Relations) == 0 {
		return false
	}
	if filter.HasConflict && !hasRelationType(rec, "conflicts-with") {
		return false
	}
	return true
}

// List browses knowledge without a search query, per §14.6's filter rail:
// type, validity state, repository, source, and staleness. search.Search
// cannot serve this - it returns nothing for an empty query.
//
// The type/state/stale/repository/source dimensions are pushed into
// knowledge.Store.ListRecords, which filters in SQL (on indexed columns and
// JSON paths) and paginates by keyset, so a first-page browse no longer loads
// the whole corpus (punokawan-rit, Phase 4 §11). HasRelation/HasConflict have
// no SQL column - they are derived from each record's embedded Relations - so
// they remain a post-filter applied to each returned page; when either is set
// this loops through pages until Limit matches are collected or the store is
// exhausted, preserving the old "limit counts post-filtered results" behavior.
func (k *KnowledgeSource) List(ctx context.Context, workspaceID string, filter contract.KnowledgeFilter) ([]protocol.KnowledgeRecord, error) {
	if err := k.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}
	store, err := k.App.OpenKnowledge()
	if err != nil {
		return nil, fmt.Errorf("sources: list knowledge: %w", err)
	}

	q := knowledge.KnowledgeListQuery{
		Type:       filter.Type,
		Repository: filter.Repository,
		Source:     filter.Source,
		Limit:      filter.Limit,
	}

	// State and Stale both constrain validity_state. State sets it directly;
	// Stale forces "stale". If both are set to different values the AND is
	// unsatisfiable, matching the old in-Go behavior of returning nothing.
	staleState := string(protocol.KnowledgeRecordValidityStateStale)
	q.ValidityState = filter.State
	if filter.Stale {
		switch {
		case q.ValidityState == "":
			q.ValidityState = staleState
		case q.ValidityState != staleState:
			return []protocol.KnowledgeRecord{}, nil
		}
	}

	postFilter := filter.HasRelation || filter.HasConflict

	out := []protocol.KnowledgeRecord{}
	cursor := ""
	for {
		q.Cursor = cursor
		page, next, err := store.ListRecords(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("sources: list knowledge: %w", err)
		}
		for _, rec := range page {
			if postFilter && !matchesKnowledgePostFilter(rec, filter) {
				continue
			}
			out = append(out, rec)
			if filter.Limit > 0 && len(out) >= filter.Limit {
				return out, nil
			}
		}
		// Without a post-filter, one page already satisfies the request: the
		// SQL LIMIT returned everything (Limit<=0) or exactly enough rows that
		// the inner return above fired. Only post-filtering can leave the page
		// short of Limit while more matches remain, so only it needs to seek on.
		if next == "" || !postFilter {
			break
		}
		cursor = next
	}
	return out, nil
}

func (k *KnowledgeSource) Search(ctx context.Context, workspaceID string, req search.Request) ([]search.Result, error) {
	if err := k.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}
	store, err := k.App.OpenKnowledge()
	if err != nil {
		return nil, fmt.Errorf("sources: search knowledge: %w", err)
	}
	ix, err := k.App.OpenSearchIndex()
	if err != nil {
		return nil, fmt.Errorf("sources: search knowledge: %w", err)
	}
	// Go through App.SearchKnowledge, not search.Search directly: it
	// watermark-gated-Rebuilds the index before querying (a no-op when the
	// store is unchanged, punokawan-77q) under the shared index lock
	// (punokawan-hzp). Without it the panel queried a stale or never-populated
	// index and returned nothing for records the store already held, so a
	// just-written record was invisible until a manual reindex (punokawan-obt).
	// This mirrors the MCP search_knowledge path exactly.
	return k.App.SearchKnowledge(store, ix, req)
}

func (k *KnowledgeSource) Get(ctx context.Context, workspaceID, knowledgeID string) (protocol.KnowledgeRecord, error) {
	if err := k.checkWorkspace(workspaceID); err != nil {
		return protocol.KnowledgeRecord{}, err
	}
	store, err := k.App.OpenKnowledge()
	if err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("sources: get knowledge %q: %w", knowledgeID, err)
	}
	return store.Get(knowledgeID)
}

func (k *KnowledgeSource) Relations(ctx context.Context, workspaceID, knowledgeID string) ([]protocol.KnowledgeRecord, error) {
	if err := k.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}
	store, err := k.App.OpenKnowledge()
	if err != nil {
		return nil, fmt.Errorf("sources: relations for knowledge %q: %w", knowledgeID, err)
	}
	return store.Related(knowledgeID)
}

// History returns knowledgeID's put/supersede/delete events in append
// order, per KnowledgeReader.History's doc comment on why this is coarser
// than the plan's full lifecycle vocabulary.
func (k *KnowledgeSource) History(ctx context.Context, workspaceID, knowledgeID string) ([]knowledge.Event, error) {
	if err := k.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}
	store, err := k.App.OpenKnowledge()
	if err != nil {
		return nil, fmt.Errorf("sources: history for knowledge %q: %w", knowledgeID, err)
	}
	all, err := store.Events()
	if err != nil {
		return nil, fmt.Errorf("sources: history for knowledge %q: %w", knowledgeID, err)
	}

	out := []knowledge.Event{}
	for _, ev := range all {
		if ev.RecordId == knowledgeID {
			out = append(out, ev)
		}
	}
	return out, nil
}
