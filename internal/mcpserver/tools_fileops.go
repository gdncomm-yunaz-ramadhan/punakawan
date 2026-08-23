package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/fileops"
	"github.com/ygrip/punakawan/internal/gitops"
)

// WriteFilesInputFile is one file in a write_files call. Path is relative
// to the task's worktree root; the caller never supplies (or needs) an
// absolute path.
type WriteFilesInputFile struct {
	Path    string `json:"path" jsonschema:"path relative to the task's worktree root"`
	Content string `json:"content" jsonschema:"the file's full new content; an existing file is overwritten, a missing one and its parent directories are created"`
}

// WriteFilesInput is write_files's input. One file and many files go
// through the same list, so there is no separate single-file surface to
// pick between.
type WriteFilesInput struct {
	RepoId string                `json:"repo_id"`
	TaskId string                `json:"task_id"`
	Files  []WriteFilesInputFile `json:"files"`
}

// WriteFilesOutputFile is one file's outcome. Error is empty on success.
type WriteFilesOutputFile struct {
	Path  string `json:"path"`
	Error string `json:"error,omitempty"`
}

// WriteFilesOutput reports every file's outcome in the order it was
// requested. A file that failed its policy check or escaped the worktree
// root carries its own error here and does not stop the rest from being
// attempted, so the caller always sees the concrete outcome of the whole
// call rather than an all-or-nothing abort.
type WriteFilesOutput struct {
	Results []WriteFilesOutputFile `json:"results"`
}

func writeFilesHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, WriteFilesInput) (*mcp.CallToolResult, WriteFilesOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in WriteFilesInput) (*mcp.CallToolResult, WriteFilesOutput, error) {
		worktreeRoot, err := gitops.WorktreePath(in.RepoId, in.TaskId)
		if err != nil {
			return nil, WriteFilesOutput{}, fmt.Errorf("mcpserver: resolve worktree path: %w", err)
		}

		specs := make([]fileops.FileSpec, len(in.Files))
		for i, f := range in.Files {
			specs[i] = fileops.FileSpec{Path: f.Path, Content: []byte(f.Content)}
		}

		results := fileops.BulkCreateFiles(a.Policy, worktreeRoot, specs)

		out := WriteFilesOutput{Results: make([]WriteFilesOutputFile, len(results))}
		for i, r := range results {
			out.Results[i] = WriteFilesOutputFile{Path: r.Path, Error: r.Error}
		}
		return nil, out, nil
	}
}
