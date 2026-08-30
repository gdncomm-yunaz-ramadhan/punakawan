package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return out
}

func subagentStopCommands(t *testing.T, settings map[string]any) []string {
	t.Helper()
	var commands []string
	hooks, _ := settings["hooks"].(map[string]any)
	groups, _ := hooks["SubagentStop"].([]any)
	for _, g := range groups {
		group, _ := g.(map[string]any)
		entries, _ := group["hooks"].([]any)
		for _, e := range entries {
			entry, _ := e.(map[string]any)
			command, _ := entry["command"].(string)
			commands = append(commands, command)
		}
	}
	return commands
}

// findRepoRootForTest walks upward from the package's own directory (go
// test's cwd) looking for go.mod, since a test's cwd is the package
// directory, not the repo root ensureUsageTrackingHook is meant to operate
// against.
func findRepoRootForTest(t *testing.T) (string, error) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}

func TestEnsureUsageTrackingHookCreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()

	changed, err := ensureUsageTrackingHook(dir)
	if err != nil {
		t.Fatalf("ensureUsageTrackingHook: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when creating a new settings.json")
	}

	settings := readSettings(t, filepath.Join(dir, ".claude", "settings.json"))
	commands := subagentStopCommands(t, settings)
	if len(commands) != 1 || commands[0] != usageTrackingHookCommand {
		t.Fatalf("SubagentStop commands = %v, want exactly [%q]", commands, usageTrackingHookCommand)
	}
}

func TestEnsureUsageTrackingHookIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	if _, err := ensureUsageTrackingHook(dir); err != nil {
		t.Fatalf("first ensureUsageTrackingHook: %v", err)
	}
	changed, err := ensureUsageTrackingHook(dir)
	if err != nil {
		t.Fatalf("second ensureUsageTrackingHook: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false on a repeat run against an already-configured file")
	}

	settings := readSettings(t, filepath.Join(dir, ".claude", "settings.json"))
	commands := subagentStopCommands(t, settings)
	if len(commands) != 1 {
		t.Fatalf("SubagentStop commands = %v, want exactly one entry (no duplicate)", commands)
	}
}

func TestEnsureUsageTrackingHookAgainstCurrentRepoSettingsReportsUnchanged(t *testing.T) {
	// This repo's own .claude/settings.json (created by the earlier
	// usage-tracking build, before setup learned to write it) already
	// carries exactly the hook this function adds. Copy it into a temp
	// dir - never write to the real file from a test - and confirm a run
	// against it reports "already configured" rather than duplicating.
	repoRoot, err := findRepoRootForTest(t)
	if err != nil {
		t.Fatalf("locate repo root: %v", err)
	}
	realSettings := filepath.Join(repoRoot, ".claude", "settings.json")
	data, err := os.ReadFile(realSettings)
	if err != nil {
		t.Skipf("repo .claude/settings.json not present (%v); skipping this specific regression check", err)
	}

	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0o644); err != nil {
		t.Fatalf("seed settings.json: %v", err)
	}

	changed, err := ensureUsageTrackingHook(dir)
	if err != nil {
		t.Fatalf("ensureUsageTrackingHook: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false against the repo's current settings.json, which already declares this hook")
	}
}

func TestEnsureClaudeCodeHooksInstallsEveryEventUsingTheInstalledBinary(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "installed", "punakawan")

	changed, err := ensureClaudeCodeHooks(dir, binaryPath)
	if err != nil {
		t.Fatalf("ensureClaudeCodeHooks: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when creating a new settings.json")
	}

	settings := readSettings(t, filepath.Join(dir, ".claude", "settings.json"))
	hooks, _ := settings["hooks"].(map[string]any)
	for _, spec := range claudeCodeHookEvents {
		groups, _ := hooks[spec.EventName].([]any)
		if len(groups) != 1 {
			t.Fatalf("event %s groups = %v, want exactly one", spec.EventName, groups)
		}
		group, _ := groups[0].(map[string]any)
		entries, _ := group["hooks"].([]any)
		if len(entries) != 1 {
			t.Fatalf("event %s hooks = %v, want exactly one", spec.EventName, entries)
		}
		entry, _ := entries[0].(map[string]any)
		if command, _ := entry["command"].(string); command != binaryPath {
			t.Fatalf("event %s command = %q, want the absolute installed binary %q (never go run)", spec.EventName, command, binaryPath)
		}
		if !ingestArgsMatch(entry["args"], "claude-code", spec.EventName) {
			t.Fatalf("event %s args = %v, want hooks ingest --client claude-code --event %s", spec.EventName, entry["args"], spec.EventName)
		}
		if spec.Async && entry["async"] != true {
			t.Fatalf("event %s expected async=true", spec.EventName)
		}
	}
}

func TestEnsureClaudeCodeHooksIsIdempotentAndPreservesUnrelatedHooks(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	seed := `{"otherTopLevelKey":"keep-me","hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"echo pretooluse"}]}]}}`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(seed), 0o644); err != nil {
		t.Fatalf("seed settings.json: %v", err)
	}

	binaryPath := filepath.Join(dir, "installed", "punakawan")
	if _, err := ensureClaudeCodeHooks(dir, binaryPath); err != nil {
		t.Fatalf("first ensureClaudeCodeHooks: %v", err)
	}
	changed, err := ensureClaudeCodeHooks(dir, binaryPath)
	if err != nil {
		t.Fatalf("second ensureClaudeCodeHooks: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false on a repeat run")
	}

	settings := readSettings(t, filepath.Join(claudeDir, "settings.json"))
	if settings["otherTopLevelKey"] != "keep-me" {
		t.Fatalf("otherTopLevelKey = %v, want preserved", settings["otherTopLevelKey"])
	}
	hooks, _ := settings["hooks"].(map[string]any)
	preToolUse, _ := hooks["PreToolUse"].([]any)
	if len(preToolUse) != 1 {
		t.Fatalf("PreToolUse groups = %v, want the original untouched entry preserved", preToolUse)
	}
	sessionStart, _ := hooks["SessionStart"].([]any)
	if len(sessionStart) != 1 {
		t.Fatalf("SessionStart groups = %v, want exactly one after two runs (no duplication)", sessionStart)
	}
}

func TestEnsureCodexHooksInstallsEveryEventUsingTheInstalledBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binaryPath := filepath.Join(home, "installed", "punakawan")

	changed, err := ensureCodexHooks(binaryPath)
	if err != nil {
		t.Fatalf("ensureCodexHooks: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when creating a new hooks.json")
	}

	settings := readSettings(t, filepath.Join(home, ".codex", "hooks.json"))
	hooks, _ := settings["hooks"].(map[string]any)
	for _, spec := range codexHookEvents {
		groups, _ := hooks[spec.EventName].([]any)
		if len(groups) != 1 {
			t.Fatalf("event %s groups = %v, want exactly one", spec.EventName, groups)
		}
		group, _ := groups[0].(map[string]any)
		entries, _ := group["hooks"].([]any)
		entry, _ := entries[0].(map[string]any)
		if command, _ := entry["command"].(string); command != binaryPath {
			t.Fatalf("event %s command = %q, want the absolute installed binary", spec.EventName, command)
		}
		if !ingestArgsMatch(entry["args"], "codex", spec.EventName) {
			t.Fatalf("event %s args = %v, want hooks ingest --client codex --event %s", spec.EventName, entry["args"], spec.EventName)
		}
	}

	changed, err = ensureCodexHooks(binaryPath)
	if err != nil {
		t.Fatalf("second ensureCodexHooks: %v", err)
	}
	if changed {
		t.Fatal("expected changed=false on a repeat run")
	}
}

func TestEnsureUsageTrackingHookPreservesUnrelatedConfig(t *testing.T) {
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	seed := `{
  "otherTopLevelKey": "keep-me",
  "hooks": {
    "PreToolUse": [
      { "hooks": [ { "type": "command", "command": "echo pretooluse" } ] }
    ],
    "SubagentStop": [
      { "hooks": [ { "type": "command", "command": "echo unrelated-subagent-stop-hook" } ] }
    ]
  }
}
`
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(seed), 0o644); err != nil {
		t.Fatalf("seed settings.json: %v", err)
	}

	changed, err := ensureUsageTrackingHook(dir)
	if err != nil {
		t.Fatalf("ensureUsageTrackingHook: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true when merging into an existing file that lacks this hook")
	}

	settings := readSettings(t, settingsPath)
	if settings["otherTopLevelKey"] != "keep-me" {
		t.Fatalf("otherTopLevelKey = %v, want preserved", settings["otherTopLevelKey"])
	}
	hooks, _ := settings["hooks"].(map[string]any)
	preToolUse, _ := hooks["PreToolUse"].([]any)
	if len(preToolUse) != 1 {
		t.Fatalf("PreToolUse groups = %v, want the original untouched entry preserved", preToolUse)
	}

	commands := subagentStopCommands(t, settings)
	wantCommands := map[string]bool{
		"echo unrelated-subagent-stop-hook": false,
		usageTrackingHookCommand:            false,
	}
	if len(commands) != 2 {
		t.Fatalf("SubagentStop commands = %v, want exactly 2 (the original plus the new one)", commands)
	}
	for _, c := range commands {
		if _, ok := wantCommands[c]; !ok {
			t.Fatalf("unexpected SubagentStop command %q", c)
		}
		wantCommands[c] = true
	}
	for c, seen := range wantCommands {
		if !seen {
			t.Fatalf("expected command %q to be present, was missing", c)
		}
	}

	// Re-run: must not duplicate the newly-added entry or disturb the
	// pre-existing unrelated one.
	changedAgain, err := ensureUsageTrackingHook(dir)
	if err != nil {
		t.Fatalf("second ensureUsageTrackingHook: %v", err)
	}
	if changedAgain {
		t.Fatal("expected changed=false on repeat run after merge")
	}
	settings = readSettings(t, settingsPath)
	commands = subagentStopCommands(t, settings)
	if len(commands) != 2 {
		t.Fatalf("SubagentStop commands after repeat run = %v, want still exactly 2", commands)
	}
}
