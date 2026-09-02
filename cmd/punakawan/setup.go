package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/ygrip/punakawan/internal/workspace"
)

func newSetupCmd() *cobra.Command {
	var hooksOnly bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Configure durable provider credentials and lifecycle hooks",
		Long: "Resolves each provider credential from the environment or the durable global credential " +
			"file, prompting interactively for anything still missing, validates every value with a real " +
			"authenticated read against the provider, and - only once validated - persists a reference to " +
			"it in the host-owned global credential file (never plaintext in a project's workspace config). " +
			"Also installs Codex and Claude Code lifecycle telemetry hooks. This never opens a subshell: a " +
			"credential exported only into one interactive shell disappears the moment that shell closes, " +
			"which is not durable. Run `punakawan doctor` afterward to verify everything end to end.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if hooksOnly {
				reportHookSetup(cmd)
				reportJiraWorkflowSetup(cmd)
				return nil
			}
			return runSetup(cmd)
		},
	}
	cmd.Flags().BoolVar(&hooksOnly, "hooks-only", false, "install lifecycle telemetry hooks only, without touching credentials (used by the non-interactive installers)")
	return cmd
}

func runSetup(cmd *cobra.Command) error {
	ctx := cmd.Context()
	out, errOut, in := cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.InOrStdin()

	envPath, err := workspace.GlobalEnvPath()
	if err != nil {
		return fmt.Errorf("setup: resolve global credential file: %w", err)
	}
	existing, err := parseEnvFile(envPath)
	if err != nil {
		return fmt.Errorf("setup: read %s: %w", envPath, err)
	}

	var failed []string
	if err := setupAtlassianCredentials(ctx, in, out, envPath, existing); err != nil {
		fmt.Fprintf(errOut, "setup: atlassian credentials not saved: %v\n", err)
		failed = append(failed, "atlassian")
	} else {
		fmt.Fprintln(out, "setup: atlassian credentials verified and saved")
	}

	if err := setupGitHubCredentials(ctx, in, out, envPath, existing); err != nil {
		fmt.Fprintf(errOut, "setup: github credentials not saved: %v\n", err)
		failed = append(failed, "github")
	} else {
		fmt.Fprintln(out, "setup: github credentials verified and saved")
	}

	reportHookSetup(cmd)
	reportJiraWorkflowSetup(cmd)

	if len(failed) > 0 {
		return fmt.Errorf("setup: could not verify credentials for: %s; set the missing/correct values (as real environment "+
			"variables or in %s) and rerun `punakawan setup`, then check `punakawan doctor --json`", strings.Join(failed, ", "), envPath)
	}
	fmt.Fprintln(out, "setup: complete; run `punakawan doctor` to verify everything end to end")
	return nil
}

// reportHookSetup ensures Codex and Claude Code both have Punakawan's full
// lifecycle telemetry hook set installed - Codex and Claude Code
// user-level for both (~/.codex/hooks.json, ~/.claude/settings.json; see
// ensureCodexHooks/ensureUserLevelClaudeCodeHooks in setup_hooks.go) plus,
// additively, the current directory's own .claude/settings.json when it
// looks like a project checkout - and prints one line per client to
// stderr reporting the outcome. It never fails setup over a hook-config
// hiccup: a tracking convenience must never block getting working,
// verified provider credentials.
func reportHookSetup(cmd *cobra.Command) {
	binaryPath, err := resolvePanelServiceBinary()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "setup: skip lifecycle hooks: could not resolve the installed punakawan binary: %v\n", err)
		return
	}

	if changed, err := ensureUserLevelClaudeCodeHooks(binaryPath); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "setup: could not configure user-level claude code lifecycle hooks: %v\n", err)
	} else if changed {
		fmt.Fprintln(cmd.ErrOrStderr(), "setup: configured user-level claude code lifecycle hooks in ~/.claude/settings.json")
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "setup: user-level claude code lifecycle hooks already configured")
	}

	if cwd, err := os.Getwd(); err == nil {
		if changed, err := ensureClaudeCodeHooks(cwd, binaryPath); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "setup: could not configure this project's claude code lifecycle hooks: %v\n", err)
		} else if changed {
			fmt.Fprintln(cmd.ErrOrStderr(), "setup: configured this project's claude code lifecycle hooks in .claude/settings.json")
		}
	}

	if changed, err := ensureCodexHooks(binaryPath); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "setup: could not configure codex lifecycle hooks: %v\n", err)
	} else if changed {
		fmt.Fprintln(cmd.ErrOrStderr(), "setup: configured codex lifecycle hooks in ~/.codex/hooks.json")
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "setup: codex lifecycle hooks already configured")
	}
}

// setupAtlassianCredentials resolves ATLASSIAN_HOST, ATLASSIAN_API_TOKEN,
// and (unless the token is scoped) ATLASSIAN_EMAIL, validates them with a
// live authenticated site/user metadata read, and persists them on
// success. It never persists a value it could not verify.
func setupAtlassianCredentials(ctx context.Context, in io.Reader, out io.Writer, envPath string, existing map[string]string) error {
	host := resolveOrPrompt(in, out, existing, "ATLASSIAN_HOST", "ATLASSIAN_HOST (for example team.atlassian.net)", false)
	token := resolveOrPrompt(in, out, existing, "ATLASSIAN_API_TOKEN", "ATLASSIAN_API_TOKEN", true)
	scoped := atlassianScoped(mergeStringMaps(existing, map[string]string{"ATLASSIAN_HOST": host, "ATLASSIAN_API_TOKEN": token}))
	var email string
	if !scoped {
		email = resolveOrPrompt(in, out, existing, "ATLASSIAN_EMAIL", "ATLASSIAN_EMAIL", false)
	}

	var missing []string
	if strings.TrimSpace(host) == "" {
		missing = append(missing, "ATLASSIAN_HOST")
	}
	if strings.TrimSpace(token) == "" {
		missing = append(missing, "ATLASSIAN_API_TOKEN")
	}
	if !scoped && strings.TrimSpace(email) == "" {
		missing = append(missing, "ATLASSIAN_EMAIL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}

	checkCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()
	if err := checkAtlassianConnectivity(checkCtx, host, token, email, scoped); err != nil {
		return fmt.Errorf("authenticated site/user metadata read against %s failed: %w", host, err)
	}

	values := map[string]string{"ATLASSIAN_HOST": host, "ATLASSIAN_API_TOKEN": token}
	if email != "" {
		values["ATLASSIAN_EMAIL"] = email
	}
	return persistEnvValues(envPath, values)
}

// setupGitHubCredentials resolves GITHUB_TOKEN, validates it with a live
// GET /user read, and persists it on success.
func setupGitHubCredentials(ctx context.Context, in io.Reader, out io.Writer, envPath string, existing map[string]string) error {
	token := resolveOrPrompt(in, out, existing, "GITHUB_TOKEN", "GITHUB_TOKEN", true)
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("missing GITHUB_TOKEN")
	}

	checkCtx, cancel := context.WithTimeout(ctx, doctorCheckTimeout)
	defer cancel()
	if err := checkGitHubConnectivity(checkCtx, token, "https://api.github.com"); err != nil {
		return fmt.Errorf("authenticated GET /user read failed: %w", err)
	}

	return persistEnvValues(envPath, map[string]string{"GITHUB_TOKEN": token})
}

// resolveOrPrompt resolves name from this process's own environment, then
// from existing (the durable global credential file's already-parsed
// contents), and only prompts interactively - masked, for secret - when
// neither has it and in is an actual interactive terminal. A
// non-interactive caller (an agent, a script, CI) with no value available
// simply gets "" back rather than hanging on a read that will never
// complete; the caller turns that into an actionable failure instead of a
// silent empty credential.
func resolveOrPrompt(in io.Reader, out io.Writer, existing map[string]string, name, prompt string, secret bool) string {
	if v, ok := os.LookupEnv(name); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := existing[name]; ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	if !isInteractiveInput(in) {
		return ""
	}

	fmt.Fprintf(out, "%s: ", prompt)
	if secret {
		if f, ok := in.(*os.File); ok {
			value, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(out)
			if err == nil {
				return strings.TrimSpace(string(value))
			}
		}
	}
	line, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(line)
}

func isInteractiveInput(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func mergeStringMaps(base, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

// persistEnvValues merges values into the KEY=value credential file at
// path, replacing an existing line for a key values names and appending a
// new one for a key it does not already have; every other line (a
// different credential, a comment, a blank line) is preserved untouched
// and in place, matching this repo's other config-merge helpers
// (setup_hooks.go's ensureIngestHooks). The file is written with 0600
// permissions: it is the durable, host-owned credential store the MCP and
// adapter launchers source before exec'ing, and it holds real secret
// values, never plaintext in any project's own workspace.yaml.
func persistEnvValues(path string, values map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("setup: create %s: %w", filepath.Dir(path), err)
	}

	existingData, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("setup: read %s: %w", path, err)
	}

	var lines []string
	if len(existingData) > 0 {
		lines = strings.Split(strings.TrimRight(string(existingData), "\n"), "\n")
	}

	remaining := make(map[string]string, len(values))
	for k, v := range values {
		remaining[k] = v
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:eq])
		if v, ok := remaining[key]; ok {
			lines[i] = key + "=" + shellQuoteEnvValue(v)
			delete(remaining, key)
		}
	}

	// Preserve insertion order for values not already present, so a
	// re-run's diff (for a human inspecting the file) stays minimal.
	for _, key := range sortedKeys(values) {
		if v, ok := remaining[key]; ok {
			lines = append(lines, key+"="+shellQuoteEnvValue(v))
		}
	}

	encoded := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		return fmt.Errorf("setup: write %s: %w", path, err)
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

// shellQuoteEnvValue wraps value in a single layer of quotes when it
// contains a character that would otherwise be reinterpreted by a POSIX
// shell `source` (Punakawan's macOS/Linux MCP launcher sources this exact
// file): single quotes unless value itself contains one, in which case
// double quotes (with embedded double quotes/backslashes escaped) - never
// both nested, since parseEnvFile only strips one matching outer layer
// back off on read. A value using only unambiguous characters (letters,
// digits, and "./-_:@") is left unquoted, which is also exactly the raw
// form scripts/install.ps1's regex-based launcher parser expects.
func shellQuoteEnvValue(value string) string {
	safe := true
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("./-_:@", r):
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return value
	}
	if !strings.Contains(value, "'") {
		return "'" + value + "'"
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return `"` + escaped + `"`
}
