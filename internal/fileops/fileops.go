// Package fileops is the file-editing abstraction described in
// punakawan-go-typescript-detailed-plan.md §3.1/§15.4/Milestone 6: Punakawan
// itself never calls an LLM (§28), so the external role proposes edits by
// calling these functions instead of writing to disk directly, which lets
// every mutation be policy-checked and confined to a single task's worktree
// rather than trusting an arbitrary caller-supplied path.
package fileops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ygrip/punakawan/internal/policy"
)

// resolveWithinRoot resolves relPath against root and refuses to return a
// path outside root, per §15.4 ("Prevent path traversal"). relPath must be
// relative; absolute paths are rejected outright rather than silently
// reinterpreted.
func resolveWithinRoot(root, relPath string) (string, error) {
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("fileops: path %q must be relative to the worktree root", relPath)
	}

	cleaned := filepath.Clean(relPath)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("fileops: path %q escapes the worktree root", relPath)
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("fileops: resolve root %q: %w", root, err)
	}

	return filepath.Join(absRoot, cleaned), nil
}

// WriteFile writes content to relPath within worktreeRoot, after confirming
// pol allows the write (§16.3 policy levels) and that relPath cannot escape
// worktreeRoot. Parent directories are created as needed.
func WriteFile(pol *policy.Policy, worktreeRoot, relPath string, content []byte) error {
	allowed, err := pol.AllowsFilesystemWrite(relPath)
	if err != nil {
		return fmt.Errorf("fileops: evaluate policy for %q: %w", relPath, err)
	}
	if !allowed {
		return fmt.Errorf("fileops: policy denies write to %q", relPath)
	}

	full, err := resolveWithinRoot(worktreeRoot, relPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("fileops: create parent directory for %q: %w", relPath, err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return fmt.Errorf("fileops: write %q: %w", relPath, err)
	}
	return nil
}

// FileSpec is one file to create in a BulkCreateFiles call.
type FileSpec struct {
	Path    string
	Content []byte
}

// FileResult is the outcome of creating one file in a BulkCreateFiles call.
type FileResult struct {
	Path  string
	Error string
}

// BulkCreateFiles writes each of files in order, applying the same checks as
// WriteFile to every entry. A failure on one file does not stop the others
// from being attempted: §10.3/§17 both expect callers to see the full,
// concrete outcome of a bulk operation rather than an all-or-nothing abort,
// so results are best-effort and per-file, not transactional.
func BulkCreateFiles(pol *policy.Policy, worktreeRoot string, files []FileSpec) []FileResult {
	results := make([]FileResult, len(files))
	for i, f := range files {
		results[i] = FileResult{Path: f.Path}
		if err := WriteFile(pol, worktreeRoot, f.Path, f.Content); err != nil {
			results[i].Error = err.Error()
		}
	}
	return results
}
