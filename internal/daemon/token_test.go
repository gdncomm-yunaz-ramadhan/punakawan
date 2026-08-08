package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateTokenGeneratesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.token")
	first, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	if len(first) != tokenBytes*2 {
		t.Fatalf("expected %d hex chars, got %d", tokenBytes*2, len(first))
	}

	second, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatalf("LoadOrCreateToken (reload): %v", err)
	}
	if second != first {
		t.Fatal("expected the same token to be returned on reload")
	}
}

func TestLoadOrCreateTokenWritesOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.token")
	if _, err := LoadOrCreateToken(path); err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("expected owner-only permissions, got %v", info.Mode().Perm())
	}
}

// TestLoadOrCreateTokenRejectsUnsafePermissions covers AC4: "unsafe
// token permissions fail closed."
func TestLoadOrCreateTokenRejectsUnsafePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.token")
	if err := os.WriteFile(path, []byte("some-token-value"), 0o644); err != nil {
		t.Fatalf("seed token file: %v", err)
	}
	if _, err := LoadOrCreateToken(path); err == nil {
		t.Fatal("expected LoadOrCreateToken to reject a world-readable token file")
	}
}
