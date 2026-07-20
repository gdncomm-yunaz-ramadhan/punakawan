package convention

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func writeFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func mkdir(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", name, err)
	}
}

// TestExtractObservedRepo builds a fixture repo with explicit config files
// (.editorconfig, go.mod + a .go file, .golangci.yml) and consistently
// kebab-case top-level directories, then asserts Extract detects all of it,
// produces a record that passes knowledge.Validate, and — since this
// fixture also has a naming-convention signal, which is always inferred
// per §27.4 — reports the overall record as inferred despite most of its
// signals being observed.
func TestExtractObservedRepo(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, ".editorconfig", "root = true\n")
	writeFile(t, dir, "go.mod", "module example.com/fixture\n\ngo 1.26\n")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, ".golangci.yml", "run:\n  timeout: 5m\n")

	mkdir(t, dir, "api-server")
	mkdir(t, dir, "web-client")
	mkdir(t, dir, "shared-lib")

	rec, err := Extract(dir, "checkout-platform", "checkout-api")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if err := knowledge.Validate(rec); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if rec.Id != "pkw:convention/checkout-platform/checkout-api" {
		t.Errorf("Id = %q, want pkw:convention/checkout-platform/checkout-api", rec.Id)
	}
	if rec.Type != protocol.KnowledgeRecordTypeConventionProfile {
		t.Errorf("Type = %q, want convention-profile", rec.Type)
	}
	if rec.Source.Provider != "git" {
		t.Errorf("Source.Provider = %q, want git", rec.Source.Provider)
	}
	if rec.Source.Uri == nil || *rec.Source.Uri != "repo://checkout-api" {
		t.Errorf("Source.Uri = %v, want repo://checkout-api", rec.Source.Uri)
	}
	if rec.Extraction.Method != protocol.KnowledgeRecordExtractionMethodModelAssisted {
		t.Errorf("Extraction.Method = %q, want model-assisted", rec.Extraction.Method)
	}

	if rec.Formatting == nil {
		t.Fatal("Formatting is nil")
	}
	if rec.Formatting.Editorconfig == nil || !*rec.Formatting.Editorconfig {
		t.Error("Formatting.Editorconfig = false/nil, want true")
	}
	if !containsStr(rec.Formatting.Linters, "golangci-lint") {
		t.Errorf("Formatting.Linters = %v, want to contain golangci-lint", rec.Formatting.Linters)
	}
	if !containsStr(rec.Formatting.Formatters, "gofmt") {
		t.Errorf("Formatting.Formatters = %v, want to contain gofmt", rec.Formatting.Formatters)
	}

	if rec.Structure == nil {
		t.Fatal("Structure is nil")
	}
	if rec.Structure.PackageManager == nil || *rec.Structure.PackageManager != "go modules" {
		t.Errorf("Structure.PackageManager = %v, want go modules", rec.Structure.PackageManager)
	}
	if rec.Structure.Layout == nil || *rec.Structure.Layout != "single" {
		t.Errorf("Structure.Layout = %v, want single", rec.Structure.Layout)
	}
	if rec.Structure.NamingConvention == nil || *rec.Structure.NamingConvention != "kebab-case-dirs" {
		t.Errorf("Structure.NamingConvention = %v, want kebab-case-dirs", rec.Structure.NamingConvention)
	}

	// The naming convention signal is inferred (no explicit config backs
	// it), so per §27.4 the overall record must not claim observed even
	// though every other signal here (.editorconfig, go.mod, .golangci.yml)
	// is an explicit config file.
	if rec.Validity.State != protocol.KnowledgeRecordValidityStateInferred {
		t.Errorf("Validity.State = %q, want inferred (naming convention is an inferred signal)", rec.Validity.State)
	}
	if rec.Extraction.Confidence == nil || *rec.Extraction.Confidence != confidenceInferred {
		t.Errorf("Extraction.Confidence = %v, want %v", rec.Extraction.Confidence, confidenceInferred)
	}
}

// TestExtractPurelyObservedRepo builds a fixture with only explicit config
// signals and no discriminating (multi-word) directory names, so naming
// convention is inconclusive and left unset — the resulting record should
// be able to claim the observed state outright.
func TestExtractPurelyObservedRepo(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, ".editorconfig", "root = true\n")
	writeFile(t, dir, "go.mod", "module example.com/fixture\n\ngo 1.26\n")
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	// Single-word directory names are compatible with every naming
	// convention simultaneously, so they don't discriminate and shouldn't
	// produce a NamingConvention verdict.
	mkdir(t, dir, "docs")
	mkdir(t, dir, "cmd")

	rec, err := Extract(dir, "ws", "repo")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if err := knowledge.Validate(rec); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if rec.Structure.NamingConvention != nil {
		t.Errorf("NamingConvention = %v, want unset (no discriminating directory names)", *rec.Structure.NamingConvention)
	}
	if rec.Validity.State != protocol.KnowledgeRecordValidityStateObserved {
		t.Errorf("Validity.State = %q, want observed", rec.Validity.State)
	}
	if rec.Extraction.Confidence == nil || *rec.Extraction.Confidence != confidenceObserved {
		t.Errorf("Extraction.Confidence = %v, want %v", rec.Extraction.Confidence, confidenceObserved)
	}
}

// TestExtractEmptyRepo asserts that a repository with no recognizable
// config files at all still produces a valid record — Extract must not
// error just because there is nothing to detect.
func TestExtractEmptyRepo(t *testing.T) {
	dir := t.TempDir()

	rec, err := Extract(dir, "ws", "empty-repo")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if err := knowledge.Validate(rec); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if rec.Formatting == nil {
		t.Fatal("Formatting is nil, want non-nil zero-value struct")
	}
	if rec.Formatting.Editorconfig != nil {
		t.Errorf("Editorconfig = %v, want nil", *rec.Formatting.Editorconfig)
	}
	if len(rec.Formatting.Linters) != 0 {
		t.Errorf("Linters = %v, want empty", rec.Formatting.Linters)
	}
	if len(rec.Formatting.Formatters) != 0 {
		t.Errorf("Formatters = %v, want empty", rec.Formatting.Formatters)
	}

	if rec.Structure == nil {
		t.Fatal("Structure is nil, want non-nil zero-value struct")
	}
	if rec.Structure.PackageManager != nil {
		t.Errorf("PackageManager = %v, want nil", *rec.Structure.PackageManager)
	}
	if rec.Structure.NamingConvention != nil {
		t.Errorf("NamingConvention = %v, want nil", *rec.Structure.NamingConvention)
	}
	// No packages/apps dir and no workspace declaration, so layout falls
	// back to the "single" default.
	if rec.Structure.Layout == nil || *rec.Structure.Layout != "single" {
		t.Errorf("Layout = %v, want single", rec.Structure.Layout)
	}

	if rec.Validity.State != protocol.KnowledgeRecordValidityStateObserved {
		t.Errorf("Validity.State = %q, want observed (no inferred signals fired)", rec.Validity.State)
	}
}

// TestExtractMonorepoLayout asserts that a packages/ directory plus a
// pnpm-workspace.yaml is recognized as a monorepo layout.
func TestExtractMonorepoLayout(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, dir, "pnpm-workspace.yaml", "packages:\n  - 'packages/*'\n")
	writeFile(t, dir, "pnpm-lock.yaml", "lockfileVersion: '9.0'\n")
	mkdir(t, dir, "packages")

	rec, err := Extract(dir, "ws", "monorepo")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if err := knowledge.Validate(rec); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if rec.Structure.Layout == nil || *rec.Structure.Layout != "monorepo" {
		t.Errorf("Layout = %v, want monorepo", rec.Structure.Layout)
	}
	if rec.Structure.PackageManager == nil || *rec.Structure.PackageManager != "pnpm" {
		t.Errorf("PackageManager = %v, want pnpm", rec.Structure.PackageManager)
	}
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
