package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRequireTrustedIfRepositoryLocalRejectsUntrustedCommand(t *testing.T) {
	repoRoot := t.TempDir()
	command := filepath.Join(repoRoot, "adapter.js")
	writeExecutable(t, command, "console.log('adapter')")

	trust, err := LoadTrustFile(filepath.Join(t.TempDir(), "adapter-trust.json"))
	if err != nil {
		t.Fatalf("LoadTrustFile: %v", err)
	}

	err = RequireTrustedIfRepositoryLocal("./adapter.js", repoRoot, trust)
	if err == nil {
		t.Fatal("expected an error for an untrusted repository-local command")
	}
	if !strings.Contains(err.Error(), "not present in the host trust file") {
		t.Fatalf("expected a trust-file error, got: %v", err)
	}
}

func TestRequireTrustedIfRepositoryLocalAcceptsTrustedCommand(t *testing.T) {
	repoRoot := t.TempDir()
	command := filepath.Join(repoRoot, "adapter.js")
	writeExecutable(t, command, "console.log('adapter')")

	// Seed the trust file under the same real (symlink-resolved) path
	// RequireTrustedIfRepositoryLocal itself will resolve command to -
	// exactly what an operator populating a real trust file would need to
	// record.
	realCommand, err := filepath.EvalSymlinks(command)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	digest, err := sha256File(realCommand)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}

	trustPath := filepath.Join(t.TempDir(), "adapter-trust.json")
	if err := SeedTrustFile(trustPath, []TrustEntry{{Path: realCommand, SHA256: digest}}); err != nil {
		t.Fatalf("SeedTrustFile: %v", err)
	}
	trust, err := LoadTrustFile(trustPath)
	if err != nil {
		t.Fatalf("LoadTrustFile: %v", err)
	}

	if err := RequireTrustedIfRepositoryLocal("./adapter.js", repoRoot, trust); err != nil {
		t.Fatalf("expected a trusted repository-local command to be accepted, got: %v", err)
	}
}

func TestRequireTrustedIfRepositoryLocalRejectsDigestMismatch(t *testing.T) {
	repoRoot := t.TempDir()
	command := filepath.Join(repoRoot, "adapter.js")
	writeExecutable(t, command, "console.log('original')")

	digest, err := sha256File(command)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}

	trustPath := filepath.Join(t.TempDir(), "adapter-trust.json")
	if err := SeedTrustFile(trustPath, []TrustEntry{{Path: command, SHA256: digest}}); err != nil {
		t.Fatalf("SeedTrustFile: %v", err)
	}
	trust, err := LoadTrustFile(trustPath)
	if err != nil {
		t.Fatalf("LoadTrustFile: %v", err)
	}

	// The file changed after it was trusted (the path still matches, but
	// the content - and therefore the digest - does not).
	writeExecutable(t, command, "console.log('tampered')")

	err = RequireTrustedIfRepositoryLocal("./adapter.js", repoRoot, trust)
	if err == nil {
		t.Fatal("expected a digest mismatch to be rejected even though the path matches")
	}
	if !strings.Contains(err.Error(), "not present in the host trust file") {
		t.Fatalf("expected a trust-file error, got: %v", err)
	}
}

func TestRequireTrustedIfRepositoryLocalIgnoresInstalledCommand(t *testing.T) {
	repoRoot := t.TempDir()
	installedDir := t.TempDir()
	installed := filepath.Join(installedDir, "adapter.js")
	writeExecutable(t, installed, "console.log('installed')")

	// No trust file at all, and no entries in it: an installed command
	// outside repoRoot needs no trust entry because it is not
	// repository-local.
	trust, err := LoadTrustFile(filepath.Join(t.TempDir(), "adapter-trust.json"))
	if err != nil {
		t.Fatalf("LoadTrustFile: %v", err)
	}

	if err := RequireTrustedIfRepositoryLocal(installed, repoRoot, trust); err != nil {
		t.Fatalf("expected an installed, non-repository-local command to need no trust entry, got: %v", err)
	}
}
