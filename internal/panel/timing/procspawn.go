package timing

// PANEL-PERF-004 - counting idle process spawns.
//
// One of Phase 0's baseline measurements is "count processes spawned during
// one minute of idle panel operation" and the §18 resource goal "idle panel
// should not spawn Beads or Git processes every second." A robust in-process
// counter would have to hook every exec.Cmd the panel launches (bd, git,
// dolt), which is invasive and, as an assertion in a test, flaky: process
// lifetimes are timing-dependent and vary by machine. So rather than build a
// brittle test, the approach is documented here for the operator to run
// against a live idle panel.
//
// Recommended manual/CI-adjacent procedure (macOS/Linux):
//
//  1. Start the panel and leave it idle (no browser interaction) after the
//     initial page load has settled.
//  2. Sample the count of owned child processes once per second for 60s:
//
//        for i in $(seq 60); do
//          pgrep -c -f 'bd|git|dolt' ;
//          sleep 1 ;
//        done | awk '{sum+=$1; n++} END {print "avg", sum/n, "samples", n}'
//
//     (Scope pgrep to the panel's process tree - e.g. `pgrep -P <panel_pid>`
//     or a cgroup on Linux - to avoid counting unrelated bd/git/dolt the
//     developer is running by hand.)
//  3. A healthy idle panel trends toward zero new spawns per sample once the
//     first snapshot has been built and cached (post Phase 1/5). A rising or
//     per-second-nonzero count is the regression this goal guards against.
//
// Record the observed average in the Phase 0 performance report alongside the
// Server-Timing baselines. When the tiered reconciliation of Phase 1
// (PANEL-PERF-009/010) lands, re-run this to confirm the one-second
// reconciler no longer performs a deep workspace listing.
