package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
)

// DeleteKnowledgeInput is delete_knowledge's input: a caller-supplied list
// of specific record ids to remove, e.g. ones a search_knowledge call
// already surfaced as stale or wrong. Naming an id is itself the deliberate
// act, so unlike a scope-wide wipe (punakawan knowledge reset, CLI-only),
// this does not need a separate confirm/dry-run gate.
type DeleteKnowledgeInput struct {
	Ids []string `json:"ids"`
}

// DeleteKnowledgeOutput is delete_knowledge's output. CommitHash is an
// opaque audit-log identifier for this delete (Store.CommitWorkingSet: a
// content hash over the commit message, not a VCS commit), empty when
// nothing was actually deleted, e.g. every id was not_found.
type DeleteKnowledgeOutput struct {
	Deleted    []string `json:"deleted"`
	NotFound   []string `json:"not_found,omitempty"`
	CommitHash string   `json:"commit_hash,omitempty"`
}

func deleteKnowledgeHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, DeleteKnowledgeInput) (*mcp.CallToolResult, DeleteKnowledgeOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in DeleteKnowledgeInput) (*mcp.CallToolResult, DeleteKnowledgeOutput, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, DeleteKnowledgeOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}
		ix, err := a.OpenSearchIndex()
		if err != nil {
			return nil, DeleteKnowledgeOutput{}, fmt.Errorf("mcpserver: open search index: %w", err)
		}

		// Deleted has no `omitempty` in its schema (the caller always wants
		// to see it), so it must marshal as `[]`, not `null`, when nothing
		// matched - a nil slice trips output-schema validation ("want array,
		// got null").
		out := DeleteKnowledgeOutput{Deleted: []string{}}
		for _, id := range in.Ids {
			if _, err := store.Get(id); err != nil {
				out.NotFound = append(out.NotFound, id)
				continue
			}
			if err := store.Delete(id); err != nil {
				return nil, DeleteKnowledgeOutput{}, fmt.Errorf("mcpserver: delete knowledge record %q: %w", id, err)
			}
			if err := ix.DeleteRecord(id); err != nil {
				return nil, DeleteKnowledgeOutput{}, fmt.Errorf("mcpserver: remove %q from search index: %w", id, err)
			}
			out.Deleted = append(out.Deleted, id)
		}
		if len(out.Deleted) > 0 {
			hash, err := store.CommitWorkingSet("punakawan: delete_knowledge " + strings.Join(out.Deleted, ", "))
			if err != nil {
				return nil, DeleteKnowledgeOutput{}, fmt.Errorf("mcpserver: commit delete_knowledge: %w", err)
			}
			out.CommitHash = hash
		}
		return nil, out, nil
	}
}

