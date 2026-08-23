// Package daemon implements Punakawan's first-use singleton daemon: one
// process per machine owns the storage kernel and serves an authenticated
// loopback transport that every CLI, MCP, and Panel client connects to
// instead of opening the database itself.
//
// Process-tree containment (cgroups/Job Objects/death-pipe guardians,
// PID-reuse-proof ownership tokens) is out of this package's scope -
// the singleton lock here is a best-effort liveness
// check sufficient to prevent two daemons racing to open the same
// database, not a hardened process-identity system.
package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// maxAcquireAttempts bounds the detect-stale/remove/retry loop in
// AcquireSingleton so a pathological repeated race (or a filesystem that
// never lets Remove succeed) fails loudly instead of spinning forever.
const maxAcquireAttempts = 20

// ErrAlreadyRunning is returned by AcquireSingleton when a live process
// already holds the lock.
type ErrAlreadyRunning struct {
	PID int
}

func (e *ErrAlreadyRunning) Error() string {
	return fmt.Sprintf("daemon: already running (pid %d)", e.PID)
}

// Lock is this process's claim on being the one daemon for path.
type Lock struct {
	path string
	PID  int
}

// AcquireSingleton claims the daemon lock at path, atomically: exactly
// one of any number of concurrent callers succeeds. A lock file left
// behind by a process that is no longer alive is treated as stale and
// removed before retrying - this is what lets a crashed daemon's next
// launch recover without manual cleanup.
func AcquireSingleton(path string) (*Lock, error) {
	for attempt := 0; attempt < maxAcquireAttempts; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			pid := os.Getpid()
			_, werr := fmt.Fprintf(f, "%d\n", pid)
			cerr := f.Close()
			if werr != nil || cerr != nil {
				os.Remove(path)
				return nil, fmt.Errorf("daemon: write lock file %s: %w", path, errors.Join(werr, cerr))
			}
			return &Lock{path: path, PID: pid}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("daemon: create lock file %s: %w", path, err)
		}

		pid, err := readLockPID(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // raced with a concurrent release; retry the create
			}
			return nil, err
		}
		if isProcessAlive(pid) {
			return nil, &ErrAlreadyRunning{PID: pid}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("daemon: remove stale lock file %s: %w", path, err)
		}
	}
	return nil, fmt.Errorf("daemon: could not acquire lock %s after %d attempts", path, maxAcquireAttempts)
}

// readLockPID reads and parses the PID recorded in an existing lock
// file, failing closed (a corrupt file is never treated as absent or
// as belonging to a specific, safely-checkable PID).
func readLockPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("daemon: corrupt lock file %s: %w", path, err)
	}
	return pid, nil
}

// Release removes the lock file. Only the process that acquired it
// should call this - releasing another process's still-live lock would
// let a second daemon start against the same database.
func (l *Lock) Release() error {
	if err := os.Remove(l.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("daemon: release lock file %s: %w", l.path, err)
	}
	return nil
}

// Status reports whether a live daemon currently holds path's lock,
// without attempting to acquire it. It never mutates the lock file.
func Status(path string) (running bool, pid int, err error) {
	pid, err = readLockPID(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, 0, nil
		}
		return false, 0, err
	}
	return isProcessAlive(pid), pid, nil
}
