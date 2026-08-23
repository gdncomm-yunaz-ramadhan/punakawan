package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAcquireSingletonSecondCallerRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	lock, err := AcquireSingleton(path)
	if err != nil {
		t.Fatalf("AcquireSingleton: %v", err)
	}
	defer lock.Release()

	_, err = AcquireSingleton(path)
	var already *ErrAlreadyRunning
	if !errors.As(err, &already) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
	if already.PID != os.Getpid() {
		t.Fatalf("expected reported pid %d, got %d", os.Getpid(), already.PID)
	}
}

// TestAcquireSingletonTwentyConcurrentCallersExactlyOneWins covers AC1:
// twenty concurrent first-use clients must produce exactly one winner.
func TestAcquireSingletonTwentyConcurrentCallersExactlyOneWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	const callers = 20
	var wins atomic.Int32
	var wg sync.WaitGroup
	locks := make([]*Lock, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lock, err := AcquireSingleton(path)
			if err == nil {
				wins.Add(1)
				locks[i] = lock
			}
		}(i)
	}
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Fatalf("expected exactly 1 winner among %d concurrent callers, got %d", callers, got)
	}
	for _, l := range locks {
		if l != nil {
			l.Release()
		}
	}
}

// TestAcquireSingletonRecoversFromStaleLock covers AC5: a lock file left
// by a process that is no longer alive must not block a fresh start.
func TestAcquireSingletonRecoversFromStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	// A pid essentially guaranteed not to be alive: os.Getpid() max is
	// bounded per-OS, but the loop below finds one that is not.
	dead := findDeadPID(t)
	if err := os.WriteFile(path, []byte(strconv.Itoa(dead)), 0o600); err != nil {
		t.Fatalf("seed stale lock file: %v", err)
	}

	lock, err := AcquireSingleton(path)
	if err != nil {
		t.Fatalf("AcquireSingleton over stale lock: %v", err)
	}
	defer lock.Release()
	if lock.PID != os.Getpid() {
		t.Fatalf("expected new lock to record this process's pid")
	}
}

func TestReleaseThenReacquire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	lock, err := AcquireSingleton(path)
	if err != nil {
		t.Fatalf("AcquireSingleton: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	second, err := AcquireSingleton(path)
	if err != nil {
		t.Fatalf("AcquireSingleton after release: %v", err)
	}
	second.Release()
}

func TestStatusReportsRunningAndAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	running, _, err := Status(path)
	if err != nil {
		t.Fatalf("Status on absent lock: %v", err)
	}
	if running {
		t.Fatal("expected not running before any lock exists")
	}

	lock, err := AcquireSingleton(path)
	if err != nil {
		t.Fatalf("AcquireSingleton: %v", err)
	}
	defer lock.Release()

	running, pid, err := Status(path)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !running || pid != os.Getpid() {
		t.Fatalf("expected running with pid %d, got running=%v pid=%d", os.Getpid(), running, pid)
	}
}

func findDeadPID(t *testing.T) int {
	t.Helper()
	for pid := 1 << 30; pid > 1; pid-- {
		if !isProcessAlive(pid) {
			return pid
		}
	}
	t.Fatal("could not find a dead pid to seed the test")
	return 0
}
