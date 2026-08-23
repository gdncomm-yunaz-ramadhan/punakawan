//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// isProcessAlive checks liveness with a signal-0 send: on POSIX systems
// os.FindProcess always succeeds regardless of whether pid exists, so
// existence must be tested by actually signaling it.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
