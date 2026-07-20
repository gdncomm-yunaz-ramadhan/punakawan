// Package tools supervises external command execution under the
// constraints in punakawan-go-typescript-detailed-plan.md §11.4: explicit
// argv (never a shell string), a working-directory allowlist, an
// environment allowlist, per-call timeouts, bounded output capture, and
// process-tree termination on cancellation.
//
// Process-group termination (Setpgid/Kill(-pid)) is POSIX-only. Windows
// support is deferred to Milestone 9 (multi-platform packaging); this
// package will not compile with GOOS=windows in its current form.
package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// DefaultEnvAllowlist is the set of environment variable names passed
// through to every supervised command, regardless of Spec.Env.
var DefaultEnvAllowlist = []string{"PATH", "HOME", "LANG", "TMPDIR"}

// Supervisor runs commands restricted to a set of allowed working-directory
// roots, an environment allowlist, a default timeout, and an output cap.
type Supervisor struct {
	AllowedRoots   []string
	EnvAllowlist   []string
	DefaultTimeout time.Duration
	MaxOutputBytes int64
}

// New returns a Supervisor restricted to the given allowed working-directory
// roots, with the package defaults for env allowlist, timeout, and output cap.
func New(allowedRoots ...string) *Supervisor {
	abs := make([]string, 0, len(allowedRoots))
	for _, r := range allowedRoots {
		if a, err := filepath.Abs(r); err == nil {
			abs = append(abs, a)
		}
	}
	return &Supervisor{
		AllowedRoots:   abs,
		EnvAllowlist:   DefaultEnvAllowlist,
		DefaultTimeout: 10 * time.Minute,
		MaxOutputBytes: 1_000_000,
	}
}

// Spec describes a single command execution request. Args are passed
// directly to the process (never interpreted by a shell).
type Spec struct {
	Name    string
	Args    []string
	Dir     string
	Env     []string
	Timeout time.Duration
}

// Result captures a completed command's outcome.
type Result struct {
	Command   string
	Args      []string
	Dir       string
	Stdout    []byte
	Stderr    []byte
	Truncated bool
	ExitCode  int
	StartedAt time.Time
	EndedAt   time.Time
}

func (s *Supervisor) checkDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("tools: resolve dir %q: %w", dir, err)
	}
	for _, root := range s.AllowedRoots {
		if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("tools: working directory %q is not within an allowed root", abs)
}

func (s *Supervisor) buildEnv(extra []string) []string {
	allow := s.EnvAllowlist
	if allow == nil {
		allow = DefaultEnvAllowlist
	}
	env := make([]string, 0, len(allow)+len(extra))
	for _, name := range allow {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return append(env, extra...)
}

// Run executes spec under this Supervisor's constraints. A non-nil error
// means the command could not be supervised at all (disallowed working
// directory, failed to start, or timeout/cancellation). A command that
// started and exited non-zero is reported via Result.ExitCode, not as an
// error.
func (s *Supervisor) Run(ctx context.Context, spec Spec) (*Result, error) {
	dir, err := s.checkDir(spec.Dir)
	if err != nil {
		return nil, err
	}

	timeout := spec.Timeout
	if timeout == 0 {
		timeout = s.DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.Name, spec.Args...)
	cmd.Dir = dir
	cmd.Env = s.buildEnv(spec.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Terminate the whole process group on cancellation, not just the
	// direct child, so grandchild processes don't outlive the timeout.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	var stdout, stderr limitedBuffer
	stdout.limit = s.MaxOutputBytes
	stderr.limit = s.MaxOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	result := &Result{
		Command:   spec.Name,
		Args:      spec.Args,
		Dir:       dir,
		StartedAt: time.Now(),
	}

	runErr := cmd.Run()
	result.EndedAt = time.Now()
	result.Stdout = stdout.buf.Bytes()
	result.Stderr = stderr.buf.Bytes()
	result.Truncated = stdout.truncated || stderr.truncated

	if runErr != nil {
		// Check context cancellation first: a process killed by our own
		// Cancel func on timeout also surfaces as an *exec.ExitError, but
		// that must be reported as a timeout, not a successful exit.
		if runCtx.Err() != nil {
			return result, fmt.Errorf("tools: %s %s: %w", spec.Name, strings.Join(spec.Args, " "), runCtx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		return result, fmt.Errorf("tools: run %s: %w", spec.Name, runErr)
	}
	return result, nil
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}
