package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/fileops"
	"github.com/ygrip/punakawan/internal/gitops"
)

// WriteFileInput is write_file's input. Path is relative to the task's
// worktree root; the caller never supplies (or needs) an absolute path.
type WriteFileInput struct {
	RepoId  string `json:"repo_id"`
	TaskId  string `json:"task_id"`
	Path    string `json:"path" jsonschema:"path relative to the task's worktree root"`
	Content string `json:"content"`
}

// WriteFileOutput is write_file's output.
type WriteFileOutput struct {
	Path string `json:"path"`
}

func writeFileHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, WriteFileInput) (*mcp.CallToolResult, WriteFileOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in WriteFileInput) (*mcp.CallToolResult, WriteFileOutput, error) {
		worktreeRoot := gitops.WorktreePath(a.Workspace.Root, in.RepoId, in.TaskId)
		if err := fileops.WriteFile(a.Policy, worktreeRoot, in.Path, []byte(in.Content)); err != nil {
			return nil, WriteFileOutput{}, fmt.Errorf("mcpserver: write_file: %w", err)
		}
		return nil, WriteFileOutput{Path: in.Path}, nil
	}
}

// BulkCreateFilesInputFile is one file in a bulk_create_files call.
type BulkCreateFilesInputFile struct {
	Path    string `json:"path" jsonschema:"path relative to the task's worktree root"`
	Content string `json:"content"`
}

// BulkCreateFilesInput is bulk_create_files's input.
type BulkCreateFilesInput struct {
	RepoId string                     `json:"repo_id"`
	TaskId string                     `json:"task_id"`
	Files  []BulkCreateFilesInputFile `json:"files"`
}

// BulkCreateFilesOutputFile is one file's outcome in bulk_create_files's
// output. Error is empty on success.
type BulkCreateFilesOutputFile struct {
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`
}

// BulkCreateFilesOutput is bulk_create_files's output.
type BulkCreateFilesOutput struct {
	Results []BulkCreateFilesOutputFile `json:"results"`
}

func bulkCreateFilesHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, BulkCreateFilesInput) (*mcp.CallToolResult, BulkCreateFilesOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in BulkCreateFilesInput) (*mcp.CallToolResult, BulkCreateFilesOutput, error) {
		worktreeRoot := gitops.WorktreePath(a.Workspace.Root, in.RepoId, in.TaskId)

		specs := make([]fileops.FileSpec, len(in.Files))
		for i, f := range in.Files {
			specs[i] = fileops.FileSpec{Path: f.Path, Content: []byte(f.Content)}
		}

		results := fileops.BulkCreateFiles(a.Policy, worktreeRoot, specs)

		out := BulkCreateFilesOutput{Results: make([]BulkCreateFilesOutputFile, len(results))}
		for i, r := range results {
			out.Results[i] = BulkCreateFilesOutputFile{Path: r.Path, Error: r.Error}
		}
		return nil, out, nil
	}
}
