package procreg

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/procident"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/tools"
)

// startOwnedProcess spawns name/args the same way any real procreg caller
// must: via tools.Supervisor, which puts the child in its own process
// group. TerminateProcessTree/KillProcessTree signal *that group*
// (kill(-pid, ...)) - a plain exec.Command.Start() without that would
// leave the child in the test binary's own process group, where a
// negative-pid kill targets a group nothing belongs to and silently no-ops.
func startOwnedProcess(t *testing.T, dir, name string, args ...string) *tools.BackgroundProcess {
	t.Helper()
	sup := tools.New(dir)
	p, err := sup.StartBackground(tools.Spec{Name: name, Args: args, Dir: dir}, filepath.Join(dir, "fixture.log"))
	if err != nil {
		t.Fatalf("StartBackground(%s): %v", name, err)
	}
	t.Cleanup(func() { p.Stop() })
	return p
}

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "procreg.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db)
}

func TestRegisterCompleteListRunning(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	rec := Record{RunID: "run-1", LeaseID: "lease-1", PID: 12345, Executable: "git", StartTime: time.Now().UTC(), OwnershipToken: "tok"}
	if err := r.Register(ctx, rec); err != nil {
		t.Fatalf("Register: %v", err)
	}

	running, err := r.ListRunning(ctx)
	if err != nil {
		t.Fatalf("ListRunning: %v", err)
	}
	if len(running) != 1 || running[0].RunID != "run-1" {
		t.Fatalf("expected [run-1], got %+v", running)
	}

	if err := r.Complete(ctx, "run-1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	running, err = r.ListRunning(ctx)
	if err != nil {
		t.Fatalf("ListRunning after Complete: %v", err)
	}
	if len(running) != 0 {
		t.Fatalf("expected no running records after Complete, got %+v", running)
	}
}

// TestReconcileKillsVerifiedSurvivor covers AC2: a record whose pid and
// start time still match a live process (a genuine orphan left behind
// by an abruptly-killed previous daemon) is terminated.
func TestReconcileKillsVerifiedSurvivor(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	p := startOwnedProcess(t, t.TempDir(), "sleep", "30")
	pid := p.Pid()

	start, err := procident.StartTime(pid)
	if err != nil {
		t.Fatalf("StartTime: %v", err)
	}
	if err := r.Register(ctx, Record{RunID: "run-survivor", PID: pid, Executable: "sleep", StartTime: start, OwnershipToken: "tok"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	result, err := r.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(result.Killed) != 1 || result.Killed[0] != "run-survivor" {
		t.Fatalf("expected run-survivor in Killed, got %+v", result)
	}
	if _, err := procident.StartTime(pid); err == nil {
		t.Fatal("expected the survivor process to be dead after Reconcile")
	}

	running, err := r.ListRunning(ctx)
	if err != nil {
		t.Fatalf("ListRunning: %v", err)
	}
	if len(running) != 0 {
		t.Fatalf("expected record marked completed after reconciliation, got %+v", running)
	}
}

// TestReconcileMarksAlreadyGoneWithoutKilling covers the case where the
// owned process already exited on its own before reconciliation runs.
func TestReconcileMarksAlreadyGoneWithoutKilling(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start true: %v", err)
	}
	pid := cmd.Process.Pid
	cmd.Wait() // ensure it has actually exited before we register it

	if err := r.Register(ctx, Record{RunID: "run-gone", PID: pid, Executable: "true", StartTime: time.Now().UTC(), OwnershipToken: "tok"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	result, err := r.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(result.AlreadyGone) != 1 || result.AlreadyGone[0] != "run-gone" {
		t.Fatalf("expected run-gone in AlreadyGone, got %+v", result)
	}
	if len(result.Killed) != 0 || len(result.Preserved) != 0 {
		t.Fatalf("expected nothing killed or preserved, got %+v", result)
	}
}

// TestReconcilePreservesIdentityMismatchWithoutKilling covers AC4: if
// the pid's current start time does not match what was recorded, the
// pid has been reused by an unrelated process, which must never be
// killed.
func TestReconcilePreservesIdentityMismatchWithoutKilling(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	p := startOwnedProcess(t, t.TempDir(), "sleep", "30")
	pid := p.Pid()

	// Register a start time that does not match the process's real
	// start time, simulating "this pid used to belong to a different,
	// now-gone process."
	wrongStart := time.Now().UTC().Add(-24 * time.Hour)
	if err := r.Register(ctx, Record{RunID: "run-mismatch", PID: pid, Executable: "sleep", StartTime: wrongStart, OwnershipToken: "tok"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	result, err := r.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(result.Preserved) != 1 || result.Preserved[0] != "run-mismatch" {
		t.Fatalf("expected run-mismatch in Preserved, got %+v", result)
	}
	if len(result.Killed) != 0 {
		t.Fatalf("expected nothing killed, got %+v", result)
	}
	if _, err := procident.StartTime(pid); err != nil {
		t.Fatal("expected the mismatched (unrelated) process to remain alive")
	}
}
