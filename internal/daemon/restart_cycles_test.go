package daemon

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"testing"
	"time"
)

// restartCycles is how many Run/Serve/Shutdown rounds this test drives.
// The release gate specifies a much longer soak (a 30-minute automated
// run, then a 24-hour release run); this is the short, CI-sized version
// that proves the same "no leak per cycle" invariant over enough
// repetitions that a per-cycle leak would show up as goroutine or
// descriptor growth, without actually running for 30 minutes.
const restartCycles = 20

// cycleClient never reuses a TCP connection across requests, so idle
// keep-alive connections from an earlier cycle's (now-shut-down) daemon
// don't sit in this client's pool looking like a leak that is actually
// just normal HTTP connection reuse.
var cycleClient = &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

// TestRepeatedRunServeShutdownCyclesLeaveNoResourceLeak runs restartCycles
// rounds of the same lifecycle TestRunServeShutdownLifecycle covers once,
// and asserts goroutine count and (where the platform supports it) open
// file descriptor count return to their pre-loop baseline afterward.
func TestRepeatedRunServeShutdownCyclesLeaveNoResourceLeak(t *testing.T) {
	// Run one throwaway cycle first so resources that only get
	// initialized on first use (e.g. the sqlite driver, DNS resolver
	// goroutines) aren't counted as "leaked" against a baseline taken
	// before any daemon has ever run.
	runOneCycle(t)
	settle()

	baselineGoroutines := runtime.NumGoroutine()
	baselineFDs := openFDCount()

	for i := 0; i < restartCycles; i++ {
		runOneCycle(t)
	}

	settle()
	if got := runtime.NumGoroutine(); got > baselineGoroutines {
		t.Fatalf("goroutine count after %d restart cycles = %d, want <= baseline %d", restartCycles, got, baselineGoroutines)
	}
	if baselineFDs >= 0 {
		if got := openFDCount(); got > baselineFDs {
			t.Fatalf("open FD count after %d restart cycles = %d, want <= baseline %d", restartCycles, got, baselineFDs)
		}
	}
}

func runOneCycle(t *testing.T) {
	t.Helper()
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()

	resp, err := cycleClient.Get("http://" + d.Addr() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

// settle gives goroutines woken by the last cycle's Shutdown (e.g.
// http.Server's own connection-closing goroutines) a moment to actually
// exit before NumGoroutine is sampled - Shutdown returning doesn't
// guarantee every goroutine it woke has finished running yet.
func settle() {
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
}

// openFDCount reports this process's open file descriptor count via
// /dev/fd, which both Linux and macOS expose, or -1 if unavailable - in
// which case the caller skips the assertion rather than failing on a
// platform without that mechanism (e.g. Windows).
func openFDCount() int {
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return -1
	}
	return len(entries)
}
