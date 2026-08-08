package tools

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTerminateProcessTreeStopsGroupLeader(t *testing.T) {
	dir := t.TempDir()
	sup := New(dir)
	p, err := sup.StartBackground(Spec{Name: "sleep", Args: []string{"30"}, Dir: dir}, filepath.Join(dir, "out.log"))
	if err != nil {
		t.Fatalf("StartBackground: %v", err)
	}
	pid := p.Pid()

	if err := TerminateProcessTree(pid); err != nil {
		t.Fatalf("TerminateProcessTree: %v", err)
	}
	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("expected process to exit after TerminateProcessTree")
	}
}

// TestKillProcessTreeRejectsUnsafePids covers a real safety gap:
// syscall.Kill(-pid, ...) with pid 0 or 1 does not target "no such
// process" - it targets the caller's entire process group (pid 0) or
// (POSIX-permitted, though not universal) every process the caller can
// signal except init (pid 1). Both must be rejected before ever
// reaching the syscall.
func TestKillProcessTreeRejectsUnsafePids(t *testing.T) {
	for _, pid := range []int{-1, 0, 1} {
		if err := TerminateProcessTree(pid); err == nil {
			t.Errorf("TerminateProcessTree(%d): expected rejection, got nil error", pid)
		}
		if err := KillProcessTree(pid); err == nil {
			t.Errorf("KillProcessTree(%d): expected rejection, got nil error", pid)
		}
	}
}
