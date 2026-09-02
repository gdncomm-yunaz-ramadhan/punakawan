//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachProcess gives the child its own process group and no console, so
// it outlives the console window that started it. CREATE_NEW_PROCESS_GROUP
// is the Windows analog of Setsid for this purpose.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
}
