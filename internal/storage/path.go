package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

const dbFileName = "punakawan.db"

// adapterTrustFileName is the host-owned file listing every
// repository-local adapter executable this host has agreed to run, keyed
// by its own normalized path and expected SHA-256 digest.
const adapterTrustFileName = "adapter-trust.json"

// dataDirOverrideEnv lets a test point the storage kernel at an isolated
// directory instead of this machine's real, shared data directory. Set
// via t.Setenv, never meant for production use - there is exactly one
// real data directory per machine by design.
const dataDirOverrideEnv = "PUNAKAWAN_DATA_DIR"

// DataDir resolves the platform-standard, per-OS-user config directory
// for Punakawan's storage kernel (XDG_CONFIG_HOME/punakawan on Linux,
// ~/Library/Application Support/punakawan on macOS, %AppData%\punakawan
// on Windows), creating it if absent.
func DataDir() (string, error) {
	dir := os.Getenv(dataDirOverrideEnv)
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("storage: resolve platform config dir: %w", err)
		}
		dir = filepath.Join(base, "punakawan")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("storage: create data dir %s: %w", dir, err)
	}
	return dir, nil
}

// DBPath returns the canonical database file path under DataDir.
func DBPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dbFileName), nil
}

// AdapterTrustFilePath returns the canonical adapter trust file path under
// DataDir. The file itself is optional - a host that has never trusted any
// repository-local adapter command simply has none on disk yet, and every
// such command is rejected until an operator adds one.
func AdapterTrustFilePath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, adapterTrustFileName), nil
}

// WorktreesDir returns the central directory execution worktrees are
// created under, creating it if absent. Worktrees are runtime state, not
// part of a managed repository, so they live here rather than inside any
// repo's own working tree.
func WorktreesDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	worktreesDir := filepath.Join(dir, "worktrees")
	if err := os.MkdirAll(worktreesDir, 0o700); err != nil {
		return "", fmt.Errorf("storage: create worktrees dir %s: %w", worktreesDir, err)
	}
	return worktreesDir, nil
}

// CheckoutsDir returns the directory clones punakawan makes on a
// caller's behalf live under, creating it if absent.
//
// A delivery names repositories, not directories, so a project nobody has
// ever opened on this machine has no checkout to work in. Cloning it here
// keeps that clone out of wherever the caller happened to be standing,
// beside the worktrees cut from it.
func CheckoutsDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	checkoutsDir := filepath.Join(dir, "checkouts")
	if err := os.MkdirAll(checkoutsDir, 0o700); err != nil {
		return "", fmt.Errorf("storage: create checkouts dir %s: %w", checkoutsDir, err)
	}
	return checkoutsDir, nil
}

// IndexesDir returns the central directory search indexes are built
// under, creating it if absent.
func IndexesDir() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	indexesDir := filepath.Join(dir, "indexes")
	if err := os.MkdirAll(indexesDir, 0o700); err != nil {
		return "", fmt.Errorf("storage: create indexes dir %s: %w", indexesDir, err)
	}
	return indexesDir, nil
}

// CheckLocation rejects database paths that live on a network-mounted
// filesystem (NFS/SMB/CIFS/AFP and similar): SQLite's file-locking
// guarantees are unreliable over network protocols and silently
// corrupt or hang under concurrent access from this kernel's writer
// and reader pools.
func CheckLocation(path string) error {
	dir := filepath.Dir(path)
	unsafe, fsType, err := isNetworkMount(dir)
	if err != nil {
		return fmt.Errorf("storage: check filesystem for %s: %w", dir, err)
	}
	if unsafe {
		return fmt.Errorf("storage: refusing network-mounted database location %s (filesystem: %s); move it to a local disk", dir, fsType)
	}
	return nil
}
