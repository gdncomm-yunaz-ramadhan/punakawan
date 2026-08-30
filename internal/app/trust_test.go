package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/storage"
)

// sha256Hex returns the lowercase hex SHA-256 digest of the file at path,
// mirroring how adapters.RequireTrustedIfRepositoryLocal hashes an adapter
// command - kept local to this test so it does not need an exported digest
// helper from internal/adapters.
func sha256Hex(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

const trustWorkspaceYAML = `version: punakawan.workspace/v1
id: trust-test-workspace
name: Trust Test Workspace
repositories:
  - id: repo-a
    path: ./repo-a
adapters:
  atlassian:
    command: ./scripts/adapter.js
`

// newRepoLocalAdapterWorkspace builds a throwaway workspace checkout whose
// declared "atlassian" adapter command lives inside the checkout itself
// (repository-local), and points PUNAKAWAN_DATA_DIR at an isolated
// directory so this test's trust file never touches a real one. It returns
// the workspace root and the adapter command's real (symlink-resolved)
// path, which is what a trust file entry must be keyed on.
func newRepoLocalAdapterWorkspace(t *testing.T) (root, realCommandPath string) {
	t.Helper()

	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".punakawan"), 0o755); err != nil {
		t.Fatalf("mkdir .punakawan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".punakawan", "workspace.yaml"), []byte(trustWorkspaceYAML), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}

	command := filepath.Join(root, "scripts", "adapter.js")
	if err := os.MkdirAll(filepath.Dir(command), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(command, []byte("console.log('adapter')"), 0o755); err != nil {
		t.Fatalf("write adapter.js: %v", err)
	}

	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())

	real, err := filepath.EvalSymlinks(command)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	return root, real
}

func TestLoadRejectsUntrustedRepositoryLocalAdapterCommand(t *testing.T) {
	root, _ := newRepoLocalAdapterWorkspace(t)

	_, err := Load(root)
	if err == nil {
		t.Fatal("expected Load to reject a repository-local adapter command with no trust file")
	}
	if !strings.Contains(err.Error(), "not present in the host trust file") {
		t.Fatalf("expected a trust-file error, got: %v", err)
	}
}

func TestLoadAcceptsTrustedRepositoryLocalAdapterCommand(t *testing.T) {
	root, realCommand := newRepoLocalAdapterWorkspace(t)

	digest := sha256Hex(t, realCommand)
	trustPath, err := storage.AdapterTrustFilePath()
	if err != nil {
		t.Fatalf("AdapterTrustFilePath: %v", err)
	}
	if err := adapters.SeedTrustFile(trustPath, []adapters.TrustEntry{{Path: realCommand, SHA256: digest}}); err != nil {
		t.Fatalf("SeedTrustFile: %v", err)
	}

	a, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	specs := a.AdapterRegistry.Specs()
	if specs["atlassian"].Command != "./scripts/adapter.js" {
		t.Fatalf("expected the configured adapter spec to survive trust validation unchanged, got %+v", specs["atlassian"])
	}
}

func TestLoadRejectsRepositoryLocalAdapterCommandOnDigestMismatch(t *testing.T) {
	root, realCommand := newRepoLocalAdapterWorkspace(t)

	digest := sha256Hex(t, realCommand)
	trustPath, err := storage.AdapterTrustFilePath()
	if err != nil {
		t.Fatalf("AdapterTrustFilePath: %v", err)
	}
	if err := adapters.SeedTrustFile(trustPath, []adapters.TrustEntry{{Path: realCommand, SHA256: digest}}); err != nil {
		t.Fatalf("SeedTrustFile: %v", err)
	}

	// The command changed after it was trusted.
	if err := os.WriteFile(realCommand, []byte("console.log('tampered')"), 0o755); err != nil {
		t.Fatalf("rewrite adapter.js: %v", err)
	}

	if _, err := Load(root); err == nil {
		t.Fatal("expected Load to reject a repository-local adapter command whose digest no longer matches")
	}
}
