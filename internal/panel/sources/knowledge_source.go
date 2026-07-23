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

func matchesKnowledgeFilter(rec protocol.KnowledgeRecord, filter contract.KnowledgeFilter) bool {
	if filter.Type != "" && string(rec.Type) != filter.Type {
		return false
	}
	if filter.State != "" && string(rec.Validity.State) != filter.State {
		return false
	}
	if filter.Stale && string(rec.Validity.State) != string(protocol.KnowledgeRecordValidityStateStale) {
		return false
	}
	if filter.Repository != "" && (rec.Scope == nil || rec.Scope.Repository == nil || *rec.Scope.Repository != filter.Repository) {
		return false
	}
	if filter.Source != "" && rec.Source.Provider != filter.Source {
		return false
	}
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
// cannot serve this - it returns nothing for an empty query - so this
// reads every record via internal/knowledge.Store.AllWithUpdatedAt and
// filters in Go.
func (k *KnowledgeSource) List(ctx context.Context, workspaceID string, filter contract.KnowledgeFilter) ([]protocol.KnowledgeRecord, error) {
	if err := k.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}
	store, err := k.App.OpenKnowledge()
	if err != nil {
		return nil, fmt.Errorf("sources: list knowledge: %w", err)
	}
	all, err := store.AllWithUpdatedAt()
	if err != nil {
		return nil, fmt.Errorf("sources: list knowledge: %w", err)
	}

	out := []protocol.KnowledgeRecord{}
	for _, entry := range all {
		if !matchesKnowledgeFilter(entry.Record, filter) {
			continue
		}
		out = append(out, entry.Record)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
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
	return search.Search(store, ix, req)
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
