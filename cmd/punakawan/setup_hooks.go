package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// usageTrackingHookCommand is the exact SubagentStop hook command punakawan's
// hook-based usage tracking relies on (see cmd/punakawan/hooks_cmd.go and
// internal/transcriptusage). Setup writes/merges this into the project's
// .claude/settings.json so a fresh install actually gets it, rather than
// requiring it to be hand-written once and never reproduced elsewhere.
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
