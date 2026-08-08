package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

const dbFileName = "punakawan.db"

// DataDir resolves the platform-standard, per-OS-user config directory
// for Punakawan's storage kernel (XDG_CONFIG_HOME/punakawan on Linux,
// ~/Library/Application Support/punakawan on macOS, %AppData%\punakawan
// on Windows), creating it if absent.
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("storage: resolve platform config dir: %w", err)
	}
	dir := filepath.Join(base, "punakawan")
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
