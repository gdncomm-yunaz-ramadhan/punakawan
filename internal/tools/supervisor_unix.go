//go:build unix

package tools

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup makes the child the leader of its own process group so the
// whole tree (child + any grandchildren it spawns) can be signalled as a unit
// via a negative pid, rather than only the direct child.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateProcessTree asks the child's whole process group to shut down
// gracefully with SIGTERM. The negative pid targets the group, so descendants
// receive the signal too.
func terminateProcessTree(p *os.Process) error {
	return syscall.Kill(-p.Pid, syscall.SIGTERM)
}

// killProcessTree forcefully kills the child's whole process group with
// SIGKILL. Used to escalate when a graceful SIGTERM does not stop the tree in
// time.
func killProcessTree(p *os.Process) error {
	return syscall.Kill(-p.Pid, syscall.SIGKILL)
}
