package tools

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStartBackgroundAndStop(t *testing.T) {
	dir := t.TempDir()
	sup := New(dir)
	logPath := filepath.Join(dir, "proc.log")

	bg, err := sup.StartBackground(Spec{Name: "sleep", Args: []string{"30"}, Dir: dir}, logPath)
	if err != nil {
		t.Fatalf("StartBackground: %v", err)
	}
	if bg.Pid() <= 0 {
		t.Fatalf("expected a positive pid, got %d", bg.Pid())
	}

	start := time.Now()
	if err := bg.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 6*time.Second {
		t.Fatalf("Stop took too long: %s", elapsed)
	}
}

func TestStartBackgroundDisallowedDir(t *testing.T) {
	allowed := t.TempDir()
	other := t.TempDir()
	sup := New(allowed)

	if _, err := sup.StartBackground(Spec{Name: "sleep", Args: []string{"5"}, Dir: other}, filepath.Join(other, "x.log")); err == nil {
		t.Fatal("expected error for a working directory outside the allowlist")
	}
}

func TestBackgroundProcessReportsEarlyExit(t *testing.T) {
	dir := t.TempDir()
	sup := New(dir)
	bg, err := sup.StartBackground(Spec{Name: "sh", Args: []string{"-c", "exit 7"}, Dir: dir}, filepath.Join(dir, "proc.log"))
	if err != nil {
		t.Fatalf("StartBackground: %v", err)
	}

	select {
	case <-bg.Done():
		if bg.WaitError() == nil {
			t.Fatal("expected the exit status to be retained")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background process exit")
	}

	// Cleanup must remain idempotent after an unexpected early exit.
	if err := bg.Stop(); err != nil {
		t.Fatalf("Stop after exit: %v", err)
	}
}
