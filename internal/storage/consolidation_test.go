package storage

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// migratedPackages are the file-based mutable stores punokawan-14yn.14/.16
// moved onto this package's shared SQLite kernel with no remaining
// file-based facet at all: internal/taskstore (14yn.14) and
// internal/approvals/internal/learning/internal/syncqueue/
// internal/panel/registry (14yn.16). A regression back to raw file I/O in
// any of them is exactly the "duplicate mutable state path" 14yn.16's goal
// says to delete, not reintroduce.
//
// internal/knowledge and internal/search (14yn.15) are deliberately NOT
// listed here even though their core stores also moved to SQLite: both
// retain legitimate, intentionally-file-based facets that predate and
// survive the migration - internal/knowledge/events.go's own append-only
// JSONL audit trail (explicitly kept separate from the Dolt/SQLite
// connection by design) and internal/knowledge/importexport.go's
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
// pass and is tracked separately (punokawan-14yn.16's AC4 calls for a
// repo-wide architectural test; this is a narrower guard on what this pass
// actually migrated, not that broader sweep).
var migratedPackages = []string{
	"../taskstore",
	"../approvals",
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
// store used, per punokawan-14yn.16 AC4 ("architectural tests find no
// mutable metadata writer outside SQLite"). A hit here means a change
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
