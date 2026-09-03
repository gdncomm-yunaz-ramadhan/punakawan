package main

import (
	"os"
	"testing"
)

// TestLoadAppOptionalSucceedsOutsideAnyProject guards the fix for
// punokawan-6s75: `punakawan mcp serve` must not require the current
// directory to be inside a project (unlike every other CLI command, which
// legitimately should via loadApp).
func TestLoadAppOptionalSucceedsOutsideAnyProject(t *testing.T) {
	dir := t.TempDir()
	prevDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(prevDir) }()

	a, err := loadApp()
	if err != nil {
		t.Fatalf("loadApp: %v", err)
	}
	defer a.Close()

	if a.Workspace == nil || !a.Workspace.Global {
		t.Fatalf("expected the global workspace outside any project, got %+v", a.Workspace)
	}
}
