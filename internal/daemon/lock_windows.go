//go:build windows

package daemon

import "os"

// isProcessAlive checks liveness via os.FindProcess: unlike POSIX,
// Windows' FindProcess opens a real handle (OpenProcess) and fails if
// pid does not currently name a live process, so success alone is a
// sufficient existence check here.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}
