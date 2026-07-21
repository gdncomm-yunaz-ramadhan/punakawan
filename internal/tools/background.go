package tools

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// BackgroundProcess is a supervised long-running process (e.g. a database
// server) started with StartBackground. Unlike Run, its lifetime is not
// bound to a single call's context; the caller controls it via Stop.
type BackgroundProcess struct {
	cmd     *exec.Cmd
	logFile *os.File
	done    chan struct{}
	mu      sync.Mutex
	waitErr error
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

	p := &BackgroundProcess{cmd: cmd, logFile: logFile, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		p.mu.Lock()
		p.waitErr = err
		p.mu.Unlock()
		close(p.done)
	}()

	return p, nil
}

// Pid returns the background process's process id.
func (p *BackgroundProcess) Pid() int {
	return p.cmd.Process.Pid
}

// Done is closed as soon as the background process exits. It lets callers
// waiting for a service to become ready fail immediately when the service
// process has already crashed, rather than waiting for a connection timeout.
func (p *BackgroundProcess) Done() <-chan struct{} {
	return p.done
}

// WaitError returns the process's exit error after Done has closed.
func (p *BackgroundProcess) WaitError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

// Stop sends SIGTERM to the process group and waits up to 5 seconds for a
// clean exit, escalating to SIGKILL if it does not stop in time.
func (p *BackgroundProcess) Stop() error {
	select {
	case <-p.done:
		return nil
	default:
	}

	pid := p.cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		select {
		case <-p.done:
			return nil
		default:
		}
		return fmt.Errorf("tools: signal process group: %w", err)
	}

	select {
	case <-p.done:
		err := p.WaitError()
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
		<-p.done
		return fmt.Errorf("tools: process did not exit within grace period; force-killed")
	}
}
