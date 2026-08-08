package procident

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestStartTimeOfSelfIsRecentAndStable(t *testing.T) {
	first, err := StartTime(os.Getpid())
	if err != nil {
		t.Fatalf("StartTime(self): %v", err)
	}
	if since := time.Since(first); since < 0 || since > time.Hour {
		t.Fatalf("expected recent start time, got %v ago", since)
	}

	second, err := StartTime(os.Getpid())
	if err != nil {
		t.Fatalf("StartTime(self) second call: %v", err)
	}
	if !first.Equal(second) {
		t.Fatalf("expected stable start time across calls, got %v then %v", first, second)
	}
}

// TestStartTimeDistinguishesDifferentProcesses is what reconciliation
// depends on: two different processes must not report the same start
// time (the actual PID-reuse case is exercised at the procreg level,
// where a real pid gets reassigned - here we just confirm the two
// child processes we can observe concurrently are distinguishable).
func TestStartTimeDistinguishesDifferentProcesses(t *testing.T) {
	a := exec.Command("sleep", "2")
	if err := a.Start(); err != nil {
		t.Fatalf("start process a: %v", err)
	}
	defer a.Process.Kill()

	time.Sleep(50 * time.Millisecond)

	b := exec.Command("sleep", "2")
	if err := b.Start(); err != nil {
		t.Fatalf("start process b: %v", err)
	}
	defer b.Process.Kill()

	tA, err := StartTime(a.Process.Pid)
	if err != nil {
		t.Fatalf("StartTime(a): %v", err)
	}
	tB, err := StartTime(b.Process.Pid)
	if err != nil {
		t.Fatalf("StartTime(b): %v", err)
	}
	if tA.Equal(tB) {
		t.Fatalf("expected distinguishable start times for two separately started processes, got %v == %v", tA, tB)
	}
}

func TestStartTimeUnknownPidErrors(t *testing.T) {
	// A pid essentially guaranteed not to exist.
	if _, err := StartTime(1 << 30); err == nil {
		t.Fatal("expected an error for a nonexistent pid")
	}
}
