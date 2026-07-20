package tools

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// BackgroundProcess is a supervised long-running process (e.g. a database
// server) started with StartBackground. Unlike Run, its lifetime is not
// bound to a single call's context; the caller controls it via Stop.
type BackgroundProcess struct {
	cmd     *exec.Cmd
	logFile *os.File
}

// StartBackground starts spec as a long-running process whose stdout and
// stderr are written to logPath, under the same working-directory and
// environment allowlist as Run. It does not wait for the process to exit.
func (s *Supervisor) StartBackground(spec Spec, logPath string) (*BackgroundProcess, error) {
	dir, err := s.checkDir(spec.Dir)
	if err != nil {
		return nil, err
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("tools: create log file %s: %w", logPath, err)
	}

	cmd := exec.Command(spec.Name, spec.Args...)
	cmd.Dir = dir
	cmd.Env = s.buildEnv(spec.Env)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("tools: start background process %s: %w", spec.Name, err)
	}

	return &BackgroundProcess{cmd: cmd, logFile: logFile}, nil
}

// Pid returns the background process's process id.
func (p *BackgroundProcess) Pid() int {
	return p.cmd.Process.Pid
}

// Stop sends SIGTERM to the process group and waits up to 5 seconds for a
// clean exit, escalating to SIGKILL if it does not stop in time.
func (p *BackgroundProcess) Stop() error {
	defer p.logFile.Close()

	pid := p.cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("tools: signal process group: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()

	select {
	case err := <-done:
		// The process exiting because of the SIGTERM we just sent is a
		// successful stop, not a failure - only surface genuinely
		// unexpected wait errors.
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			return fmt.Errorf("tools: wait for process exit: %w", err)
		}
		return nil
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-done
		return fmt.Errorf("tools: process did not exit within grace period; force-killed")
	}
}
