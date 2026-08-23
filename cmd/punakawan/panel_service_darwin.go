//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// launchdPanelService registers the panel as a per-user LaunchAgent.
// A user agent (rather than a system daemon under /Library) is the
// right level here: the panel serves one person's workspaces on
// loopback, needs that person's home directory and login keychain, and
// installing it requires no administrator rights.
type launchdPanelService struct {
	label     string
	plistPath string
	// domain is the launchd domain target, "gui/<uid>", which is the
	// per-user GUI session the LaunchAgent belongs to.
	domain string
}

func newPanelServiceManager() (panelServiceManager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("panel service: resolve home directory: %w", err)
	}
	return &launchdPanelService{
		label:     panelServiceLabel,
		plistPath: filepath.Join(home, "Library", "LaunchAgents", panelServiceLabel+".plist"),
		domain:    fmt.Sprintf("gui/%d", os.Getuid()),
	}, nil
}

func (s *launchdPanelService) serviceTarget() string { return s.domain + "/" + s.label }

// Install is deliberately re-runnable: it rewrites the plist and
// reloads the job, so changing --port is just another install rather
// than an uninstall/install dance, and a second install never leaves
// two registrations behind.
func (s *launchdPanelService) Install(spec panelServiceSpec) error {
	if err := ensurePlistIsOurs(s.plistPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.plistPath), 0o755); err != nil {
		return fmt.Errorf("panel service: create %s: %w", filepath.Dir(s.plistPath), err)
	}
	if err := writeFileAtomic(s.plistPath, []byte(renderPanelServicePlist(spec)), 0o644); err != nil {
		return fmt.Errorf("panel service: write %s: %w", s.plistPath, err)
	}

	// Booting out first is what makes a re-install pick up the new
	// plist: launchd keeps the definition it loaded, so an already
	// bootstrapped job would otherwise keep serving the old arguments.
	// A job that was not loaded makes this a no-op, hence the ignored
	// error.
	_, _ = s.launchctl("bootout", s.serviceTarget())

	if out, err := s.launchctl("bootstrap", s.domain, s.plistPath); err != nil {
		return fmt.Errorf("panel service: launchctl bootstrap %s: %w%s", s.plistPath, err, formatLaunchctlOutput(out))
	}
	return nil
}

// Uninstall tolerates every partial state - job loaded but plist gone,
// plist present but job never loaded, nothing at all - so running it
// twice is not an error.
func (s *launchdPanelService) Uninstall() error {
	if err := ensurePlistIsOurs(s.plistPath); err != nil {
		return err
	}

	if out, err := s.launchctl("bootout", s.serviceTarget()); err != nil && !isLaunchctlNotFound(out) {
		return fmt.Errorf("panel service: launchctl bootout %s: %w%s", s.serviceTarget(), err, formatLaunchctlOutput(out))
	}
	if err := os.Remove(s.plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("panel service: remove %s: %w", s.plistPath, err)
	}
	return nil
}

// Start loads an already-installed definition without rewriting it, so
// it is the counterpart to Stop rather than a second install.
func (s *launchdPanelService) Start() error {
	if err := s.requireInstalled(); err != nil {
		return err
	}
	if _, loaded, _ := s.printService(); loaded {
		// Already bootstrapped. KeepAlive means launchd owns restarts,
		// so kickstart is only needed to nudge a job that is loaded but
		// not currently up.
		if out, err := s.launchctl("kickstart", s.serviceTarget()); err != nil {
			return fmt.Errorf("panel service: launchctl kickstart %s: %w%s", s.serviceTarget(), err, formatLaunchctlOutput(out))
		}
		return nil
	}
	if out, err := s.launchctl("bootstrap", s.domain, s.plistPath); err != nil {
		return fmt.Errorf("panel service: launchctl bootstrap %s: %w%s", s.plistPath, err, formatLaunchctlOutput(out))
	}
	return nil
}

// Stop boots the job out of the launchd domain but leaves the plist in
// place, which is what keeps the service registered. Signalling the
// process instead would be pointless: KeepAlive would simply bring it
// straight back.
func (s *launchdPanelService) Stop() error {
	if err := s.requireInstalled(); err != nil {
		return err
	}
	if out, err := s.launchctl("bootout", s.serviceTarget()); err != nil && !isLaunchctlNotFound(out) {
		return fmt.Errorf("panel service: launchctl bootout %s: %w%s", s.serviceTarget(), err, formatLaunchctlOutput(out))
	}
	return nil
}

func (s *launchdPanelService) Status() (panelServiceStatus, error) {
	status := panelServiceStatus{DefinitionPath: s.plistPath}

	content, err := os.ReadFile(s.plistPath)
	switch {
	case err == nil:
		status.Registered = true
		status.Host, status.Port = panelServiceAddressFromPlist(string(content))
		if label := plistDeclaredLabel(string(content)); label != s.label {
			status.Registered = false
			status.Detail = fmt.Sprintf("a file at %s declares label %q, not %q - it is not managed by Punakawan", s.plistPath, label, s.label)
			return status, nil
		}
	case os.IsNotExist(err):
	default:
		return status, fmt.Errorf("panel service: read %s: %w", s.plistPath, err)
	}

	out, loaded, err := s.printService()
	if err != nil {
		return status, err
	}
	if !loaded {
		if status.Registered {
			status.Detail = "installed but not loaded in launchd; run `punakawan panel service start`"
		}
		return status, nil
	}
	if pid, ok := launchctlPID(out); ok {
		status.Running = true
		status.PID = pid
	} else {
		status.Detail = "loaded in launchd but no process is running; check the panel service logs"
	}
	return status, nil
}

func (s *launchdPanelService) requireInstalled() error {
	if err := ensurePlistIsOurs(s.plistPath); err != nil {
		return err
	}
	if _, err := os.Stat(s.plistPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("panel service: not installed (%s does not exist); run `punakawan panel service install` first", s.plistPath)
		}
		return fmt.Errorf("panel service: stat %s: %w", s.plistPath, err)
	}
	return nil
}

// printService asks launchd about the job. A non-zero exit means the
// job is not loaded in this domain, which is an answer rather than a
// failure, so only the inability to run launchctl at all is an error.
func (s *launchdPanelService) printService() (output string, loaded bool, err error) {
	out, runErr := s.launchctl("print", s.serviceTarget())
	if runErr == nil {
		return out, true, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return out, false, fmt.Errorf("panel service: run launchctl print %s: %w", s.serviceTarget(), runErr)
	}
	return out, false, nil
}

func (s *launchdPanelService) launchctl(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var launchctlPIDPattern = regexp.MustCompile(`(?m)^\s*pid\s*=\s*(\d+)`)

func launchctlPID(printOutput string) (int, bool) {
	m := launchctlPIDPattern.FindStringSubmatch(printOutput)
	if m == nil {
		return 0, false
	}
	pid, err := strconv.Atoi(m[1])
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// isLaunchctlNotFound recognises launchctl's way of saying the job is
// not loaded, which every teardown path should treat as success.
func isLaunchctlNotFound(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "could not find service") ||
		strings.Contains(lower, "no such process") ||
		strings.Contains(lower, "not find specified service")
}

func formatLaunchctlOutput(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	return "\n" + out
}
