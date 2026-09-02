package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ygrip/punakawan/internal/storage"
)

// panelModulePath mirrors the module path declared in go.mod. The
// service identity is derived from it rather than hand-written so the
// two cannot drift into naming the same install twice under different
// identifiers.
const panelModulePath = "github.com/ygrip/punakawan"

// panelServiceLabel identifies the background panel job to the host's
// service manager - on macOS it is both the launchd job label and the
// base name of the LaunchAgent plist. Reverse-DNS is what launchd
// expects, and deriving it from the module path keeps it unmistakably
// this binary's rather than something that could collide with an
// unrelated tool's agent.
var panelServiceLabel = reverseDNSLabel(panelModulePath) + ".panel"

// reverseDNSLabel converts a Go module path into a reverse-DNS
// identifier: "github.com/ygrip/punakawan" becomes
// "com.github.ygrip.punakawan". Only the host component is reversed -
// the path segments after it stay in their original order, since they
// read as an increasingly specific name, exactly like the trailing
// components of a bundle identifier. Characters that are not safe in a
// service label are folded to "-" so an unusual module path still
// yields something a service manager will accept.
func reverseDNSLabel(modulePath string) string {
	segments := strings.FieldsFunc(modulePath, func(r rune) bool { return r == '/' })
	if len(segments) == 0 {
		return ""
	}

	hostParts := strings.Split(segments[0], ".")
	parts := make([]string, 0, len(hostParts)+len(segments)-1)
	for i := len(hostParts) - 1; i >= 0; i-- {
		parts = append(parts, hostParts[i])
	}
	parts = append(parts, segments[1:]...)

	safe := parts[:0]
	for _, part := range parts {
		part = sanitizeLabelPart(part)
		if part != "" {
			safe = append(safe, part)
		}
	}
	return strings.Join(safe, ".")
}

func sanitizeLabelPart(part string) string {
	var b strings.Builder
	for _, r := range part {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// panelServiceSpec is everything the service definition needs to launch
// a panel that behaves the same as a foreground `punakawan panel`, minus
// the terminal.
type panelServiceSpec struct {
	// BinaryPath is the absolute, symlink-resolved path of the
	// punakawan executable. A service started at login cannot rely on
	// the interactive shell's PATH, so the full path is baked in.
	BinaryPath string
	// Host and Port mirror the `panel` flags of the same name.
	Host string
	Port string
	// WorkspacePath is absolute for the same reason BinaryPath is: the
	// service manager starts the job with its own working directory,
	// not the one the user happened to be in when installing.
	WorkspacePath string
	StdoutPath    string
	StderrPath    string
}

// programArguments is the exact argv the service runs. It stays in the
// foreground because the service manager is the supervisor here - a job
// that daemonized itself would exit immediately and be restarted forever.
// Browser opening is forced off: a job that starts at login must not
// hijack the session by launching a browser window nobody asked for.
func (s panelServiceSpec) programArguments() []string {
	return []string{
		s.BinaryPath,
		"panel",
		"--foreground",
		"--host", s.Host,
		"--port", s.Port,
		"--workspace", s.WorkspacePath,
		"--open-browser=false",
	}
}

func (s panelServiceSpec) address() string {
	return fmt.Sprintf("http://%s:%s", s.Host, s.Port)
}

// panelServiceStatus is the platform-neutral answer to "is this thing
// set up, and is it up right now?".
type panelServiceStatus struct {
	// Registered means a service definition exists on disk. Running
	// means the service manager currently has a live process for it.
	Registered bool
	Running    bool
	PID        int
	// DefinitionPath is where the definition lives, for a user who
	// wants to inspect or remove it by hand.
	DefinitionPath string
	Host           string
	Port           string
	// Detail carries whatever the service manager said, for the cases
	// where "not running" alone is not enough to act on.
	Detail string
}

// panelServiceManager is the per-platform half of the panel service
// commands. Everything above this line is shared; everything behind it
// talks to launchd, systemd, or whatever the host actually uses.
type panelServiceManager interface {
	Install(spec panelServiceSpec) error
	Uninstall() error
	Start() error
	Stop() error
	Status() (panelServiceStatus, error)
}

// resolvePanelServiceBinary returns the absolute path of the executable
// currently running, with symlinks resolved. Resolving symlinks matters
// because the common install shapes - a Homebrew shim, a `go install`
// binary symlinked onto PATH - would otherwise bake in a path that a
// later upgrade can repoint or delete underneath the service.
func resolvePanelServiceBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("panel service: locate this executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("panel service: resolve %s: %w", exe, err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("panel service: absolute path of %s: %w", resolved, err)
	}
	return abs, nil
}

// panelServiceLogPaths puts the service's stdout and stderr next to the
// rest of this install's runtime state (the same directory the daemon
// keeps its lock, token and database in) rather than inventing a
// separate log location.
func panelServiceLogPaths() (stdoutPath, stderrPath string, err error) {
	dir, err := storage.DataDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "panel-service.out.log"), filepath.Join(dir, "panel-service.err.log"), nil
}

// errPanelServiceUnsupported explains, per platform, what the missing
// implementation would have been. Naming the mechanism is the point:
// the user can go set it up by hand today instead of being told only
// that this does not work here.
func errPanelServiceUnsupported(goos string) error {
	switch goos {
	case "linux":
		return fmt.Errorf("panel service: not implemented on linux yet. " +
			"The equivalent mechanism is a systemd user unit - a ~/.config/systemd/user/punakawan-panel.service " +
			"file driven by `systemctl --user enable --now punakawan-panel` (with `loginctl enable-linger` " +
			"if you want it up without an active login session). Until Punakawan writes that unit for you, " +
			"run `punakawan panel` under a supervisor of your choice")
	case "windows":
		return fmt.Errorf("panel service: not implemented on windows yet. " +
			"The equivalent mechanism is a Task Scheduler logon task (`schtasks /create /sc onlogon /tn Punakawan Panel " +
			"/tr \"<path-to-punakawan.exe> panel --open-browser=false\"`) or a service wrapper such as NSSM or WinSW " +
			"if you want a real Windows service with restart-on-failure. Until Punakawan registers that for you, " +
			"set it up by hand or run `punakawan panel` in a persistent session")
	default:
		return fmt.Errorf("panel service: not implemented on %s. "+
			"Only macOS is supported today, via a launchd user LaunchAgent. "+
			"Run `punakawan panel` under a supervisor appropriate to this platform instead", goos)
	}
}

// renderPanelServicePlist produces the launchd LaunchAgent property
// list for spec. It lives outside the darwin-only file so it can be
// unit-tested on any host without a launchd to talk to.
//
// RunAtLoad plus KeepAlive is what makes the panel "always active":
// launchd starts it when the user logs in and restarts it whenever it
// exits, crash or not. ProcessType Background tells the scheduler this
// is not an interactive app, so it is not given foreground priority.
func renderPanelServicePlist(spec panelServiceSpec) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n")
	b.WriteString("<dict>\n")

	writePlistString(&b, "Label", panelServiceLabel)

	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, arg := range spec.programArguments() {
		b.WriteString("\t\t<string>" + escapeXML(arg) + "</string>\n")
	}
	b.WriteString("\t</array>\n")

	writePlistString(&b, "WorkingDirectory", spec.WorkspacePath)
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<true/>\n")
	writePlistString(&b, "ProcessType", "Background")
	writePlistString(&b, "StandardOutPath", spec.StdoutPath)
	writePlistString(&b, "StandardErrorPath", spec.StderrPath)

	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func writePlistString(b *strings.Builder, key, value string) {
	b.WriteString("\t<key>" + escapeXML(key) + "</key>\n")
	b.WriteString("\t<string>" + escapeXML(value) + "</string>\n")
}

func escapeXML(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		// EscapeText only fails if the writer fails, and bytes.Buffer
		// never does.
		return s
	}
	return buf.String()
}

var (
	plistLabelPattern  = regexp.MustCompile(`(?s)<key>Label</key>\s*<string>(.*?)</string>`)
	plistStringPattern = regexp.MustCompile(`<string>([^<]*)</string>`)
)

// plistDeclaredLabel returns the Label a plist declares, or "" if it
// declares none.
func plistDeclaredLabel(content string) string {
	m := plistLabelPattern.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return unescapeXML(m[1])
}

// panelServiceAddressFromPlist recovers the host and port a previously
// installed service was registered with, so `status` can report where
// the panel actually listens instead of guessing the defaults.
func panelServiceAddressFromPlist(content string) (host, port string) {
	values := make([]string, 0, 8)
	for _, m := range plistStringPattern.FindAllStringSubmatch(content, -1) {
		values = append(values, unescapeXML(m[1]))
	}
	for i := 0; i+1 < len(values); i++ {
		switch values[i] {
		case "--host":
			host = values[i+1]
		case "--port":
			port = values[i+1]
		}
	}
	return host, port
}

var xmlUnescaper = strings.NewReplacer(
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", `"`,
	"&apos;", "'",
	"&#39;", "'",
	"&#34;", `"`,
	"&#xA;", "\n",
	"&#xD;", "\r",
	"&#x9;", "\t",
	// &amp; is expanded last so "&amp;lt;" survives as the literal
	// "&lt;" instead of collapsing into "<".
	"&amp;", "&",
)

func unescapeXML(s string) string { return xmlUnescaper.Replace(s) }

// ensurePlistIsOurs guards the plist path before install or uninstall
// touches it. A file sitting at our label's path that does not declare
// our label is something a person wrote by hand, and clobbering it
// would destroy work this command has no claim to.
func ensurePlistIsOurs(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("panel service: read %s: %w", path, err)
	}
	if label := plistDeclaredLabel(string(content)); label != panelServiceLabel {
		return fmt.Errorf("panel service: refusing to touch %s: it declares label %q, not %q, "+
			"so it was not written by this command - move or delete it yourself if you want "+
			"Punakawan to manage that path",
			path, label, panelServiceLabel)
	}
	return nil
}

// writeFileAtomic replaces path in one step so a service manager
// reading the file concurrently never sees a half-written definition.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
