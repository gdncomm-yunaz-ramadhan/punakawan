package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// usageTrackingHookCommand is the exact SubagentStop hook command an
// earlier release installed. It is superseded by ensureClaudeCodeHooks'
// full lifecycle mapping (every event, the absolute installed binary,
// exec form - never `go run`, never a path inside the consumer's
// repository), which is what a fresh setup now installs. This constant
// and ensureUsageTrackingHook remain only so a project's existing
// settings.json - and the "hooks record-usage" alias it points at (see
// cmd/punakawan/hooks_cmd.go) - are still recognized/left alone rather
// than duplicated; setup no longer calls ensureUsageTrackingHook itself.
const usageTrackingHookCommand = `cd "${CLAUDE_PROJECT_DIR}" && go run ./cmd/punakawan hooks record-usage`

// ensureUsageTrackingHook makes sure projectRoot's .claude/settings.json
// declares the SubagentStop usage-tracking hook, creating the file (and its
// .claude directory) if neither exists yet. Every other key in an existing
// settings.json - other hooks, other SubagentStop entries, anything else at
// all - is preserved untouched; this only ever adds the one entry it owns,
// and only if an equivalent one (matched by command string, not deep-equal)
// isn't already present, so repeated runs are idempotent. changed reports
// whether the file was actually created or modified.
func ensureUsageTrackingHook(projectRoot string) (changed bool, err error) {
	settingsDir := filepath.Join(projectRoot, ".claude")
	settingsPath := filepath.Join(settingsDir, "settings.json")

	var root map[string]any
	existing, readErr := os.ReadFile(settingsPath)
	switch {
	case readErr == nil:
		if err := json.Unmarshal(existing, &root); err != nil {
			return false, fmt.Errorf("setup: parse %s: %w", settingsPath, err)
		}
	case os.IsNotExist(readErr):
		root = map[string]any{}
	default:
		return false, fmt.Errorf("setup: read %s: %w", settingsPath, readErr)
	}
	if root == nil {
		root = map[string]any{}
	}

	if hasUsageTrackingHook(root) {
		return false, nil
	}

	addUsageTrackingHook(root)

	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, fmt.Errorf("setup: encode %s: %w", settingsPath, err)
	}
	encoded = append(encoded, '\n')

	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return false, fmt.Errorf("setup: create %s: %w", settingsDir, err)
	}
	if err := os.WriteFile(settingsPath, encoded, 0o644); err != nil {
		return false, fmt.Errorf("setup: write %s: %w", settingsPath, err)
	}
	return true, nil
}

// hookEventSpec names one client lifecycle event `hooks ingest` supports
// and whether it should be declared async in the client's own hook
// config (a slow spool write must never block the agent's own turn for
// an event that isn't already on the client's own synchronous budget).
type hookEventSpec struct {
	EventName string
	Async     bool
}

// claudeCodeHookEvents is every event clienthooks.ParseClaudeEvent maps.
// SessionStart/SessionEnd are not async: SessionStart's session identity
// must exist before any later event in the same session can resolve it,
// and Claude Code's own SessionEnd budget (documented as a short,
// non-blocking window) already caps how long a synchronous SessionEnd
// hook may run.
var claudeCodeHookEvents = []hookEventSpec{
	{EventName: "SessionStart"},
	{EventName: "PostToolUse", Async: true},
	{EventName: "PostToolUseFailure", Async: true},
	{EventName: "SubagentStart", Async: true},
	{EventName: "SubagentStop", Async: true},
	{EventName: "Stop", Async: true},
	{EventName: "StopFailure", Async: true},
	{EventName: "SessionEnd"},
}

// codexHookEvents is every event clienthooks.ParseCodexEvent maps. Codex
// documents SessionEnd as running synchronously regardless of an `async`
// flag, so it is declared the same way as Claude Code's.
var codexHookEvents = []hookEventSpec{
	{EventName: "SessionStart"},
	{EventName: "PostToolUse", Async: true},
	{EventName: "SubagentStart", Async: true},
	{EventName: "SubagentStop", Async: true},
	{EventName: "Stop", Async: true},
	{EventName: "SessionEnd"},
}

// ensureClaudeCodeHooks installs punakawan's full Claude Code lifecycle
// hook set into projectRoot's .claude/settings.json: every event in
// claudeCodeHookEvents, each an exec-form command entry (no shell, no
// ${CLAUDE_PROJECT_DIR} expansion) naming binaryPath - the absolute,
// already-installed punakawan binary (see resolvePanelServiceBinary) -
// and its `hooks ingest --client claude-code --event <EventName>`
// arguments directly, never `go run` and never a path inside the
// consumer's own repository. Every other existing key, hook, or event
// group is preserved untouched; an event already declaring an equivalent
// entry (matched by exact command+args, not deep-equal) is left alone, so
// repeated runs are idempotent.
func ensureClaudeCodeHooks(projectRoot, binaryPath string) (bool, error) {
	return ensureIngestHooks(filepath.Join(projectRoot, ".claude", "settings.json"), binaryPath, "claude-code", claudeCodeHookEvents)
}

// ensureCodexHooks installs punakawan's full Codex lifecycle hook set
// into ~/.codex/hooks.json (one of the config locations Codex documents
// for hook registration), the same way ensureClaudeCodeHooks does for
// Claude Code.
func ensureCodexHooks(binaryPath string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("setup: resolve home directory: %w", err)
	}
	return ensureIngestHooks(filepath.Join(home, ".codex", "hooks.json"), binaryPath, "codex", codexHookEvents)
}

// ensureIngestHooks is ensureClaudeCodeHooks/ensureCodexHooks' shared
// merge logic: both clients document the identical
// {"hooks": {"EventName": [{"hooks": [...]}]}} JSON config shape.
func ensureIngestHooks(configPath, binaryPath, clientKind string, events []hookEventSpec) (bool, error) {
	var root map[string]any
	existing, readErr := os.ReadFile(configPath)
	switch {
	case readErr == nil:
		if err := json.Unmarshal(existing, &root); err != nil {
			return false, fmt.Errorf("setup: parse %s: %w", configPath, err)
		}
	case os.IsNotExist(readErr):
		root = map[string]any{}
	default:
		return false, fmt.Errorf("setup: read %s: %w", configPath, readErr)
	}
	if root == nil {
		root = map[string]any{}
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}

	changed := false
	for _, spec := range events {
		if hasIngestHookEntry(hooks, spec.EventName, binaryPath, clientKind) {
			continue
		}
		addIngestHookEntry(hooks, spec, binaryPath, clientKind)
		changed = true
	}
	if !changed {
		return false, nil
	}

	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return false, fmt.Errorf("setup: encode %s: %w", configPath, err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return false, fmt.Errorf("setup: create %s: %w", filepath.Dir(configPath), err)
	}
	if err := os.WriteFile(configPath, encoded, 0o644); err != nil {
		return false, fmt.Errorf("setup: write %s: %w", configPath, err)
	}
	return true, nil
}

func hasIngestHookEntry(hooks map[string]any, eventName, binaryPath, clientKind string) bool {
	groups, _ := hooks[eventName].([]any)
	for _, g := range groups {
		group, _ := g.(map[string]any)
		if group == nil {
			continue
		}
		entries, _ := group["hooks"].([]any)
		for _, e := range entries {
			entry, _ := e.(map[string]any)
			if entry == nil {
				continue
			}
			command, _ := entry["command"].(string)
			if command != binaryPath {
				continue
			}
			if ingestArgsMatch(entry["args"], clientKind, eventName) {
				return true
			}
		}
	}
	return false
}

func ingestArgsMatch(raw any, clientKind, eventName string) bool {
	args, _ := raw.([]any)
	want := []string{"hooks", "ingest", "--client", clientKind, "--event", eventName}
	if len(args) != len(want) {
		return false
	}
	for i, w := range want {
		s, ok := args[i].(string)
		if !ok || s != w {
			return false
		}
	}
	return true
}

func addIngestHookEntry(hooks map[string]any, spec hookEventSpec, binaryPath, clientKind string) {
	groups, _ := hooks[spec.EventName].([]any)
	entry := map[string]any{
		"type":    "command",
		"command": binaryPath,
		"args":    []any{"hooks", "ingest", "--client", clientKind, "--event", spec.EventName},
	}
	if spec.Async {
		entry["async"] = true
	}
	groups = append(groups, map[string]any{"hooks": []any{entry}})
	hooks[spec.EventName] = groups
}

// hasUsageTrackingHook reports whether root's hooks.SubagentStop array
// already contains a hook entry whose command matches
// usageTrackingHookCommand. It tolerates any shape variance a hand-edited
// settings.json might have (missing keys, non-array/non-object values from
// a malformed file) by simply treating anything unexpected as "not found"
// rather than failing - a settings.json this command can't confidently
// parse this deeply is one it should leave alone and just append safely to.
func hasUsageTrackingHook(root map[string]any) bool {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return false
	}
	groups, _ := hooks["SubagentStop"].([]any)
	for _, g := range groups {
		group, _ := g.(map[string]any)
		if group == nil {
			continue
		}
		entries, _ := group["hooks"].([]any)
		for _, e := range entries {
			entry, _ := e.(map[string]any)
			if entry == nil {
				continue
			}
			command, _ := entry["command"].(string)
			if strings.TrimSpace(command) == usageTrackingHookCommand {
				return true
			}
		}
	}
	return false
}

// addUsageTrackingHook appends one new SubagentStop hook group carrying
// punakawan's usage-tracking command to root, creating "hooks" and
// "hooks.SubagentStop" if they don't already exist. It never removes or
// reorders anything already present.
func addUsageTrackingHook(root map[string]any) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	groups, _ := hooks["SubagentStop"].([]any)
	groups = append(groups, map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": usageTrackingHookCommand,
				"async":   true,
			},
		},
	})
	hooks["SubagentStop"] = groups
}
