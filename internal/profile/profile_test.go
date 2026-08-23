package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsEmptyProfile(t *testing.T) {
	root := t.TempDir()

	rp, err := Load(root)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if len(rp.Entries) != 0 {
		t.Fatalf("Load: expected no entries, got %v", rp.Entries)
	}
	if _, ok := rp.Value("anything"); ok {
		t.Fatalf("Value: expected no match on empty profile")
	}
}

func TestLoadParsesEntries(t *testing.T) {
	root := t.TempDir()
	writeRepoProfile(t, root, `
version: punakawan.repo-profile/v1
entries:
  - key: build.tool
    value: bazel
  - key: retry.max_attempts
    value: 3
`)

	rp, err := Load(root)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if len(rp.Entries) != 2 {
		t.Fatalf("Load: expected 2 entries, got %d", len(rp.Entries))
	}

	v, ok := rp.Value("build.tool")
	if !ok || v != "bazel" {
		t.Fatalf("Value(build.tool) = %v, %v; want bazel, true", v, ok)
	}

	// Case-insensitive lookup, matching internal/project's MetadataFor.
	v, ok = rp.Value("BUILD.TOOL")
	if !ok || v != "bazel" {
		t.Fatalf("Value(BUILD.TOOL) = %v, %v; want bazel, true", v, ok)
	}

	if _, ok := rp.Value("unknown.key"); ok {
		t.Fatalf("Value(unknown.key): expected no match")
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	root := t.TempDir()
	writeRepoProfile(t, root, `
version: punakawan.repo-profile/v99
entries: []
`)

	if _, err := Load(root); err == nil {
		t.Fatalf("Load: expected an error for an unsupported version")
	}
}

func writeRepoProfile(t *testing.T, root, contents string) {
	t.Helper()
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, configFile)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
