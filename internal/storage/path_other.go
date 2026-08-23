//go:build !darwin && !linux && !windows

package storage

// isNetworkMount has no platform-specific detection on this GOOS; the
// kernel is only required to build and pass on darwin, linux, and
// windows, so unsupported platforms fail open (never block Open) rather
// than guess.
func isNetworkMount(dir string) (bool, string, error) {
	return false, "", nil
}
