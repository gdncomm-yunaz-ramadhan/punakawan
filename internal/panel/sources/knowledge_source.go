package sources

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/app"
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
		return fmt.Errorf("sources: workspace %q is not available (only %q is)", workspaceID, k.App.Workspace.ID)
	}
	return nil
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
