package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSearchFindsMatch(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "example.txt"), []byte("hello punakawan\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	sup := New(dir)
	res, err := sup.Search(context.Background(), dir, []string{"punakawan", "."}, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0 (match found), got %d; stderr=%s", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(string(res.Stdout), "hello punakawan") {
		t.Fatalf("expected match in output, got: %s", res.Stdout)
	}
}

func TestSearchNoMatch(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not installed")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "example.txt"), []byte("nothing relevant\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	sup := New(dir)
	res, err := sup.Search(context.Background(), dir, []string{"no-such-pattern", "."}, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// ripgrep exits 1 when no match is found; that's a supervised result, not an error.
	if res.ExitCode != 1 {
		t.Fatalf("expected exit 1 (no match), got %d", res.ExitCode)
	}
}
