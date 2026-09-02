package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTailLines(t *testing.T) {
	const log = "one\ntwo\nthree\nfour\n"
	for _, tc := range []struct {
		n    int
		want string
	}{
		{2, "three\nfour"},
		{10, "one\ntwo\nthree\nfour"},
		{0, ""},
	} {
		if got := tailLines(log, tc.n); got != tc.want {
			t.Errorf("tailLines(n=%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
	if got := tailLines("no trailing newline", 1); got != "no trailing newline" {
		t.Errorf("tailLines without a trailing newline = %q", got)
	}
}

func TestPanelRecordRoundTripAndLiveness(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())

	if _, running, err := readPanelRecord(); err != nil || running {
		t.Fatalf("with no record: running = %v, err = %v, want false/nil", running, err)
	}

	// This test's own process is the one live pid it can name without
	// starting anything.
	want := panelRecord{PID: os.Getpid(), Host: "127.0.0.1", Port: "7331", Workspace: "/tmp/ws", StartedAt: time.Now().UTC()}
	if err := writePanelRecord(want); err != nil {
		t.Fatalf("writePanelRecord: %v", err)
	}
	got, running, err := readPanelRecord()
	if err != nil {
		t.Fatalf("readPanelRecord: %v", err)
	}
	if !running {
		t.Error("a record naming this live process should read as running")
	}
	if got.Host != want.Host || got.Port != want.Port || got.Workspace != want.Workspace {
		t.Errorf("record = %+v, want %+v", got, want)
	}
	if got.address() != "http://127.0.0.1:7331" {
		t.Errorf("address = %q", got.address())
	}

	// A record naming a pid nothing is using reads as not running rather
	// than as a panel that cannot be reached.
	if err := writePanelRecord(panelRecord{PID: 0}); err != nil {
		t.Fatalf("writePanelRecord: %v", err)
	}
	if _, running, _ := readPanelRecord(); running {
		t.Error("a record naming no process should not read as running")
	}
}

func TestPanelLogsCommandExplainsAnEmptyLog(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	cmd := newPanelLogsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("panel logs: %v", err)
	}
	if !strings.Contains(out.String(), "punakawan panel") {
		t.Errorf("an empty log should name the command that creates one, got: %q", out.String())
	}
}

// The launchd job is supervised by launchd, so it must not daemonize
// itself: a job that forks and exits is a job launchd restarts forever.
func TestPanelServiceRunsTheServerInTheForeground(t *testing.T) {
	args := panelServiceSpec{BinaryPath: "/usr/local/bin/punakawan", Host: "127.0.0.1", Port: "7331", WorkspacePath: "/tmp/ws"}.programArguments()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--foreground") {
		t.Errorf("service argv = %q, want --foreground", joined)
	}
}
