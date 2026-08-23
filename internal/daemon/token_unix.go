//go:build !windows

package daemon

import (
	"fmt"
	"os"
)

// checkTokenFilePermissions fails closed if the token file is readable
// or writable by anyone other than its owner.
func checkTokenFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("daemon: stat token file %s: %w", path, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("daemon: refusing token file %s with unsafe permissions %v (must be owner-only)", path, info.Mode().Perm())
	}
	return nil
}
