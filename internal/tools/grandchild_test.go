//go:build unix

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestKillProcessTreeCatchesGrandchild covers punokawan-14yn.18 AC3: a
// fixture that forks a grandchild must be completely contained, not
// just its direct child. sh backgrounds a grandchild sleep (inheriting
// sh's process group, since plain `&` does not call setsid) and writes
// its pid to a file so the test can verify that pid is dead too, not
// just the direct child sh.
func TestKillProcessTreeCatchesGrandchild(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	script := fmt.Sprintf("sleep 30 & echo $! > %s; wait", pidFile)

	sup := New(dir)
	p, err := sup.StartBackground(Spec{Name: "sh", Args: []string{"-c", script}, Dir: dir}, filepath.Join(dir, "fixture.log"))
	if err != nil {
		t.Fatalf("StartBackground: %v", err)
	}

	grandchildPid := waitForGrandchildPid(t, pidFile)

	if err := KillProcessTree(p.Pid()); err != nil {
		t.Fatalf("KillProcessTree: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected direct child (sh) to exit after KillProcessTree")
	}

	if isAlive(grandchildPid) {
		t.Fatalf("expected grandchild pid %d to be dead after KillProcessTree(parent)", grandchildPid)
	}
}

func waitForGrandchildPid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			var pid int
			fmt.Sscanf(string(data), "%d", &pid)
			if pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("grandchild pid file was never written")
	return 0
}

func isAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
