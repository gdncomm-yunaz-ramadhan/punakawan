package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// DeleteKnowledgeInput is delete_knowledge's input: a caller-supplied list
// of specific record ids to remove, e.g. ones a search_knowledge call
// already surfaced as stale or wrong. Naming an id is itself the deliberate
// act, so unlike reset_project_knowledge's wildcard scope wipe, this does
// not need a separate confirm/dry-run gate.
type DeleteKnowledgeInput struct {
	Ids []string `json:"ids"`
}

// DeleteKnowledgeOutput is delete_knowledge's output.
type DeleteKnowledgeOutput struct {
	Deleted  []string `json:"deleted"`
	NotFound []string `json:"not_found,omitempty"`
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

		out := DeleteKnowledgeOutput{}
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
		return nil, out, nil
	}
}

// ResetProjectKnowledgeInput is reset_project_knowledge's input: a bulk
// wipe of every record matching the given scope (§10.4's scope.project/
// repository/module). Unlike delete_knowledge's explicit id list, a scope
// filter's blast radius is not obvious to the caller ahead of time, so
// Confirm defaults to a dry-run: it must be explicitly set true to actually
// delete anything, mirroring push_task_branch's AllowPush and
// resolve_review_thread's Allow fields elsewhere in this codebase.
type ResetProjectKnowledgeInput struct {
	Project    string `json:"project,omitempty"`
	Repository string `json:"repository,omitempty"`
	Module     string `json:"module,omitempty"`

	Confirm bool `json:"confirm,omitempty" jsonschema:"must be true to actually delete the matched records; false (default) returns a dry-run preview only"`
}

// ResetProjectKnowledgeOutput is reset_project_knowledge's output.
type ResetProjectKnowledgeOutput struct {
	MatchedIds []string `json:"matched_ids"`
	Deleted    bool     `json:"deleted"`
	Reason     string   `json:"reason,omitempty"`
}

func resetProjectKnowledgeHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ResetProjectKnowledgeInput) (*mcp.CallToolResult, ResetProjectKnowledgeOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ResetProjectKnowledgeInput) (*mcp.CallToolResult, ResetProjectKnowledgeOutput, error) {
		if in.Project == "" && in.Repository == "" && in.Module == "" {
			return nil, ResetProjectKnowledgeOutput{}, fmt.Errorf("mcpserver: reset_project_knowledge requires at least one of project/repository/module - an empty scope would match every record in the store")
		}

		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, ResetProjectKnowledgeOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		matched, err := matchingScope(store, in)
		if err != nil {
			return nil, ResetProjectKnowledgeOutput{}, err
		}

		if !in.Confirm {
			return nil, ResetProjectKnowledgeOutput{
				MatchedIds: matched,
				Deleted:    false,
				Reason:     fmt.Sprintf("dry run: %d record(s) match this scope; call again with confirm=true to delete them", len(matched)),
			}, nil
		}

		ix, err := a.OpenSearchIndex()
		if err != nil {
			return nil, ResetProjectKnowledgeOutput{}, fmt.Errorf("mcpserver: open search index: %w", err)
		}
		for _, id := range matched {
			if err := store.Delete(id); err != nil {
				return nil, ResetProjectKnowledgeOutput{}, fmt.Errorf("mcpserver: delete knowledge record %q: %w", id, err)
			}
			if err := ix.DeleteRecord(id); err != nil {
				return nil, ResetProjectKnowledgeOutput{}, fmt.Errorf("mcpserver: remove %q from search index: %w", id, err)
			}
		}
		return nil, ResetProjectKnowledgeOutput{MatchedIds: matched, Deleted: true}, nil
	}
}

// matchingScope returns every record's id whose scope matches every
// non-empty field in.Project/Repository/Module requests. Unset request
// fields are not filtered on.
func matchingScope(store *knowledge.Store, in ResetProjectKnowledgeInput) ([]string, error) {
	records, err := store.AllWithUpdatedAt()
	if err != nil {
		return nil, fmt.Errorf("mcpserver: list knowledge records: %w", err)
	}

	var ids []string
	for _, rec := range records {
		if scopeMatches(rec.Record.Scope, in) {
			ids = append(ids, rec.Record.Id)
		}
	}
	return ids, nil
}

func scopeMatches(scope *protocol.KnowledgeRecordScope, in ResetProjectKnowledgeInput) bool {
	if in.Project != "" && (scope == nil || scope.Project == nil || *scope.Project != in.Project) {
		return false
	}
	if in.Repository != "" && (scope == nil || scope.Repository == nil || *scope.Repository != in.Repository) {
		return false
	}
	if in.Module != "" && (scope == nil || scope.Module == nil || *scope.Module != in.Module) {
		return false
	}
	return true
}
