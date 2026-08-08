package tools

import (
	"fmt"
	"os"
)

// checkPid rejects any pid that the underlying per-OS implementation
// would treat as something other than "one specific process I own."
// On unix, terminateProcessTree/killProcessTree signal the process
// *group* via kill(-pid, ...): pid 0 there means "my own process group"
// and pid 1 means "every process the caller can signal" (POSIX kill(2))
// - neither is ever the intended target of an owned-process kill, so
// both are rejected here rather than left for the syscall layer to
// interpret literally.
func checkPid(pid int) error {
	if pid <= 1 {
		return fmt.Errorf("tools: refusing to signal pid %d (not a specific owned process)", pid)
	}
	return nil
}

// TerminateProcessTree requests a graceful shutdown of pid and its
// descendants (SIGTERM on unix, a non-forceful taskkill /T on
// Windows), reusing the same per-OS logic Supervisor.Run and
// BackgroundProcess.Stop use internally. Exported so callers that only
// have a bare pid on hand - e.g. punokawan-14yn.18's restart-time
// reconciliation, which reads pids back out of a durable process
// registry rather than holding an *exec.Cmd - don't have to
// reimplement process-tree termination.
func TerminateProcessTree(pid int) error {
	if err := checkPid(pid); err != nil {
		return err
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return terminateProcessTree(p)
}

// KillProcessTree forcefully terminates pid and its descendants.
func KillProcessTree(pid int) error {
	if err := checkPid(pid); err != nil {
		return err
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return killProcessTree(p)
}
