//go:build windows

package tools

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// Windows has no POSIX process groups or signals, so the supervisor cannot
// signal a whole process tree with a negative pid the way it does on unix.
// Instead the child is launched in a new process group (so a stray Ctrl-C in
// the parent's console does not knock it out) and the tree is torn down with
// the taskkill utility, which walks and terminates the child plus every
// descendant it can find.
//
// Limitations vs the POSIX backend:
//   - Graceful termination is best-effort. taskkill without /F sends a
//     WM_CLOSE-style request that only windowed processes honour; console
//     children often ignore it. The BackgroundProcess.Stop grace period then
//     escalates to a forced taskkill (/F), which is the reliable path on
//     Windows. This is coarser than the unix SIGTERM->SIGKILL escalation.
//   - Tree discovery relies on taskkill /T (parent/child PID relationships).
//     A grandchild that has been reparented (its parent already exited) may
//     escape termination, whereas a POSIX process group would still catch it.
//   - taskkill must be on PATH (it ships with Windows). If it cannot run, the
//     forced path falls back to killing the direct child process only.

// createNewProcessGroup is CREATE_NEW_PROCESS_GROUP; putting the child in its
// own group detaches it from the parent console's Ctrl-C/Ctrl-Break handling.
const createNewProcessGroup = 0x00000200

// setProcessGroup starts the child in its own process group so it does not
// share the parent's console control events.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// terminateProcessTree makes a best-effort graceful request for the child and
// its descendants to exit (taskkill /T, without /F). It intentionally returns
// nil even when taskkill reports the tree could not be closed gracefully, so
// callers escalate to killProcessTree after their grace period rather than
// treating the graceful attempt as a hard failure.
func terminateProcessTree(p *os.Process) error {
	_ = runTaskkill(p.Pid, false)
	return nil
}

// killProcessTree forcefully terminates the child and its descendants
// (taskkill /F /T). If taskkill cannot run at all, it falls back to killing
// the direct child so the process does not linger indefinitely.
func killProcessTree(p *os.Process) error {
	if err := runTaskkill(p.Pid, true); err != nil {
		return p.Kill()
	}
	return nil
}

// runTaskkill invokes the Windows taskkill utility against pid. With force it
// passes /F (forceful) in addition to /T (terminate the whole tree).
func runTaskkill(pid int, force bool) error {
	args := []string{"/T", "/PID", strconv.Itoa(pid)}
	if force {
		args = append([]string{"/F"}, args...)
	}
	out, err := exec.Command("taskkill", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tools: taskkill %v: %w (%s)", args, err, out)
	}
	return nil
}
