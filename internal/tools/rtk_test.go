package tools

import (
	"context"
	"os/exec"
	"testing"
)

func TestRTKAvailable(t *testing.T) {
	if _, err := exec.LookPath("rtk"); err != nil {
		t.Skip("rtk not installed")
	}

	dir := t.TempDir()
	sup := New(dir)
	if !sup.RTKAvailable(context.Background(), dir) {
		t.Fatal("expected RTKAvailable to be true when rtk is on PATH")
	}
}

func TestRunViaRTK(t *testing.T) {
	if _, err := exec.LookPath("rtk"); err != nil {
		t.Skip("rtk not installed")
	}

	dir := t.TempDir()
	sup := New(dir)
	res, err := sup.RunViaRTK(context.Background(), dir, []string{"--version"}, 0)
	if err != nil {
		t.Fatalf("RunViaRTK: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%s", res.ExitCode, res.Stderr)
	}
}
