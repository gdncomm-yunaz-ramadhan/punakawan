package storage

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// migratedPackages are the file-based mutable stores moved onto this
// package's shared SQLite kernel with no remaining file-based facet at all:
// internal/learning, internal/syncqueue, internal/panel/registry.
// internal/approvals was one such package too, until the execution-approval
// contract it backed was removed outright (not merely migrated) and the
// whole package was deleted. A regression back to raw file I/O in any
// package still listed here is exactly the "duplicate mutable state path"
// this migration was meant to delete, not reintroduce.
//
// internal/knowledge and internal/search are deliberately NOT listed here
// even though their core stores also moved to SQLite: both retain
// legitimate, intentionally-file-based facets that predate and survive the
// migration - internal/knowledge/events.go's own append-only JSONL audit
// trail (explicitly kept separate from the Dolt/SQLite connection by
// design) and internal/knowledge/importexport.go's
// Export/ExportYAML/ImportYAML data-portability feature (backup/restore,
// not where canonical data lives day to day). Including them here would
// just be a permanent false positive, not a real regression signal.
//
// This also does NOT enumerate every remaining os.WriteFile call site in
// the repo (internal/artifact's content-addressed proposal bytes,
// internal/evidence/internal/testrun's immutable evidence, internal/
// workflow/internal/dossier's explicitly out-of-scope ceremony, and several
// disposable caches/settings files are all legitimate file-based stores
// today) - correctly classifying all of those was out of reach for this
// pass and is tracked as a separate, broader repo-wide architectural test;
// this is a narrower guard on what this pass actually migrated, not that
// broader sweep.
var migratedPackages = []string{
	"../learning",
	"../syncqueue",
	"../panel/registry",
}

// forbiddenFileIO matches the write-oriented file operations these packages
// used before their SQLite migration: creating/appending/writing a file
// directly, or opening a jsonl/yaml file for read (the read side no longer
// belongs here either, since there is nothing left for it to read).
var forbiddenFileIO = regexp.MustCompile(`\bos\.(WriteFile|OpenFile|Create)\b|\.jsonl["'` + "`" + `]|gopkg\.in/yaml`)

// TestMigratedPackagesHaveNoFileBasedStore statically scans each migrated
// package's non-test source for the raw file-I/O patterns its old JSONL/YAML
// store used - architectural tests must find no mutable metadata writer
// outside SQLite. A hit here means a change
// reintroduced file-based persistence into a package the storage kernel
// migration was supposed to make SQLite-only.
func TestMigratedPackagesHaveNoFileBasedStore(t *testing.T) {
	for _, dir := range migratedPackages {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			// legacy_import.go is the one-time, read-and-rename migration facet
			// that pulls any pre-kernel JSONL/YAML file a workspace still has on
			// disk into the SQLite kernel on first open, then leaves it renamed
			// as a backup. It legitimately references the old file paths and (for
			// registry) the YAML decoder to read them once - it is not a
			// persistence path, so it is excluded here, mirroring how the whole
			// knowledge/search packages are excluded for their file-based
			// import/export and audit facets above.
			if name == "legacy_import.go" {
				continue
			}
			path := filepath.Join(dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if loc := forbiddenFileIO.FindIndex(data); loc != nil {
				line := 1 + strings.Count(string(data[:loc[0]]), "\n")
				t.Errorf("%s:%d: matches a pre-migration file-store pattern (%q) - this package's persistence must go through the shared SQLite kernel, not a file", path, line, forbiddenFileIO.FindString(string(data)))
			}
		}
	}
}
