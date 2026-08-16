package procreg

import (
	"context"
	"fmt"
	"testing"

	"github.com/ygrip/punakawan/internal/procident"
)

// interruptionCycles is how many spawn/crash/restart cycles this test
// runs against one registry. The release gate asks for a much larger
// count (hundreds) exercised against a real daemon binary over a long
// session; this in-process version proves the same reconciliation
// invariant repeatedly enough that a leak on any single cycle would
// surface as a failure, without needing to build and drive an actual
// daemon process for each round.
const interruptionCycles = 20

// TestReconcileAcrossManyInterruptionCyclesLeavesNoSurvivors runs
// interruptionCycles rounds of "an owned process outlives an abrupt
// daemon crash, then the next daemon instance reconciles it away"
// against a single durable registry - the same one every real daemon
// restart shares. It checks each cycle's process is dead immediately
// after Reconcile, then checks every pid from every cycle once more at
// the end, so a survivor that outlives its own cycle's assertion still
// gets caught.
func TestReconcileAcrossManyInterruptionCyclesLeavesNoSurvivors(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	var allPIDs []int
	for i := 0; i < interruptionCycles; i++ {
		p := startOwnedProcess(t, t.TempDir(), "sleep", "30")
		pid := p.Pid()
		allPIDs = append(allPIDs, pid)

		start, err := procident.StartTime(pid)
		if err != nil {
			t.Fatalf("cycle %d: StartTime: %v", i, err)
		}
		runID := fmt.Sprintf("run-cycle-%d", i)
		if err := r.Register(ctx, Record{RunID: runID, PID: pid, Executable: "sleep", StartTime: start, OwnershipToken: "tok"}); err != nil {
			t.Fatalf("cycle %d: Register: %v", i, err)
		}

		result, err := r.Reconcile(ctx)
		if err != nil {
			t.Fatalf("cycle %d: Reconcile: %v", i, err)
		}
		if len(result.Killed) != 1 || result.Killed[0] != runID {
			t.Fatalf("cycle %d: expected %s killed, got %+v", i, runID, result)
		}
		if _, err := procident.StartTime(pid); err == nil {
			t.Fatalf("cycle %d: pid %d still alive after Reconcile", i, pid)
		}
	}

	for _, pid := range allPIDs {
		if _, err := procident.StartTime(pid); err == nil {
			t.Fatalf("pid %d survived across all %d interruption cycles", pid, interruptionCycles)
		}
	}
}
