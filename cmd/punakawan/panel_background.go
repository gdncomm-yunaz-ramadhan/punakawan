package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// The panel is a dashboard, not a terminal program: a person starts it,
// looks at it in a browser, and expects the terminal back. So `punakawan
// panel` re-executes this binary detached, records where it went, and
// returns - while `panel logs` reads what it has printed since. The
// foreground mode it re-executes into is still the whole implementation;
// nothing about the server changes.

// panelRecord is what a running detached panel leaves behind so a later
// command can find it. It doubles as the pid file: the file existing and
// naming a live process is what "running" means.
type panelRecord struct {
	PID       int       `json:"pid"`
	Host      string    `json:"host"`
	Port      string    `json:"port"`
	Workspace string    `json:"workspace"`
	StartedAt time.Time `json:"started_at"`
	LogPath   string    `json:"log_path"`
}

func (r panelRecord) address() string { return "http://" + net.JoinHostPort(r.Host, r.Port) }

// panelRecordPath and panelLogPath live beside the rest of this install's
// runtime state, next to the daemon's lock and database rather than in a
// log directory of their own.
func panelRecordPath() (string, error) {
	dir, err := storage.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "panel.json"), nil
}

func panelLogPath() (string, error) {
	dir, err := storage.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "panel.log"), nil
}

// readPanelRecord returns the recorded panel and whether its process is
// still alive. A record naming a dead process is reported as not running
// rather than deleted: `panel logs` after a crash is exactly when knowing
// what was running matters most.
func readPanelRecord() (panelRecord, bool, error) {
	path, err := panelRecordPath()
	if err != nil {
		return panelRecord{}, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return panelRecord{}, false, nil
		}
		return panelRecord{}, false, fmt.Errorf("panel: read %s: %w", path, err)
	}
	var record panelRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return panelRecord{}, false, fmt.Errorf("panel: parse %s: %w", path, err)
	}
	return record, processAlive(record.PID), nil
}

func writePanelRecord(record panelRecord) error {
	path, err := panelRecordPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

func removePanelRecord() error {
	path, err := panelRecordPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// processAlive reports whether pid names a process this user can signal.
// Signal 0 performs the permission and existence checks without
// delivering anything.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// addressAnswers reports whether something is already listening. It is
// how `panel` avoids starting a second server behind the launchd service
// (or behind a panel someone started from another checkout): the port
// being taken is the honest signal, whoever owns it.
func addressAnswers(host, port string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// startPanelDetached re-executes this binary in foreground panel mode
// with its output redirected to the log file, in its own session so it
// survives the terminal that started it. It returns once the panel is
// actually answering, so the address printed to the user is one they can
// open immediately rather than one that may still be booting.
func startPanelDetached(serve panelServeFlags) (panelRecord, error) {
	binary, err := resolvePanelServiceBinary()
	if err != nil {
		return panelRecord{}, err
	}
	workspace := serve.workspacePath
	if workspace == "" {
		if workspace, err = os.Getwd(); err != nil {
			return panelRecord{}, err
		}
	}
	if workspace, err = filepath.Abs(workspace); err != nil {
		return panelRecord{}, err
	}

	logPath, err := panelLogPath()
	if err != nil {
		return panelRecord{}, err
	}
	// Appended rather than truncated: a panel that died on startup an
	// hour ago is still the thing a person is trying to read about.
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return panelRecord{}, fmt.Errorf("panel: open log %s: %w", logPath, err)
	}
	defer logFile.Close()
	fmt.Fprintf(logFile, "\n=== panel started %s ===\n", time.Now().Format(time.RFC3339))

	cmd := exec.Command(binary, "panel",
		"--foreground",
		"--host", serve.host,
		"--port", serve.port,
		"--workspace", workspace,
		"--open-browser=false",
	)
	cmd.Dir = workspace
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	// Detaching the child from this terminal's session is what makes
	// closing the terminal - or Ctrl-C in it - leave the panel running.
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return panelRecord{}, fmt.Errorf("panel: start detached: %w", err)
	}
	// Released rather than waited on: this command is about to exit, and
	// an unreaped child would otherwise be re-parented mid-startup.
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()

	record := panelRecord{
		PID: pid, Host: serve.host, Port: serve.port,
		Workspace: workspace, StartedAt: time.Now().UTC(), LogPath: logPath,
	}
	// Generous, because the panel's own startup includes discovering the
	// daemon, which waits on its health check before giving up.
	if err := waitForPanel(record, 45*time.Second); err != nil {
		return record, err
	}
	if err := writePanelRecord(record); err != nil {
		return record, err
	}
	return record, nil
}

// waitForPanel blocks until the panel answers or the process is gone. A
// panel that exited during startup fails here with its own log tail
// attached, rather than leaving the user to discover an address that
// refuses connections.
func waitForPanel(record panelRecord, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if addressAnswers(record.Host, record.Port) {
			return nil
		}
		if !processAlive(record.PID) {
			return fmt.Errorf("panel: exited during startup:\n%s", lastPanelLogLines(20))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("panel: did not answer on %s within %s; see `punakawan panel logs`", record.address(), timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stopPanel asks the recorded panel to shut down and waits for it to go.
// A panel that is already gone is the desired end state, not an error.
func stopPanel(timeout time.Duration) (panelRecord, bool, error) {
	record, running, err := readPanelRecord()
	if err != nil {
		return record, false, err
	}
	if !running {
		return record, false, removePanelRecord()
	}
	proc, err := os.FindProcess(record.PID)
	if err != nil {
		return record, false, err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return record, false, fmt.Errorf("panel: stop pid %d: %w", record.PID, err)
	}
	deadline := time.Now().Add(timeout)
	for processAlive(record.PID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(record.PID) {
		return record, true, fmt.Errorf("panel: pid %d did not stop within %s", record.PID, timeout)
	}
	return record, true, removePanelRecord()
}

// lastPanelLogLines returns the tail of the panel log, for an error
// message that would otherwise say only that something failed.
func lastPanelLogLines(n int) string {
	path, err := panelLogPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return tailLines(string(data), n)
}

// tailLines returns the last n lines of s.
func tailLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	end := len(s)
	if end > 0 && s[end-1] == '\n' {
		end--
	}
	count := 0
	for i := end - 1; i >= 0; i-- {
		if s[i] != '\n' {
			continue
		}
		count++
		if count == n {
			return s[i+1 : end]
		}
	}
	return s[:end]
}

// followPanelLog streams the log file as it grows, the way `tail -f`
// does, until ctx-bound interruption closes the process.
func followPanelLog(out io.Writer, from int64) error {
	path, err := panelLogPath()
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(from, io.SeekStart); err != nil {
		return err
	}
	buf := make([]byte, 4096)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if err != nil {
			return err
		}
	}
}
