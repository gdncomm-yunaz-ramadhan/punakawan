// Package settings persists the panel's runtime-tunable configuration — the
// values a user can change from the System panel without restarting or editing
// a file by hand. Today that is the bounded per-project runtime pool: how many
// project workspaces may have a live *app.App at once, and how long an idle
// one lingers before it is shut down.
//
// Settings live at <primaryRoot>/.punakawan/panel/settings.json. A missing or
// unreadable file is a normal state, not an error: Load returns the defaults.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// DefaultMaxActiveRuntimes caps how many project runtimes the panel keeps
	// live at once, including the primary.
	DefaultMaxActiveRuntimes = 4
	// DefaultRuntimeIdleTimeoutSeconds is how long a non-primary, unused runtime
	// may sit idle before the panel shuts it down (12 minutes).
	DefaultRuntimeIdleTimeoutSeconds = 720
)

// Settings is the persisted panel configuration.
type Settings struct {
	// MaxActiveRuntimes bounds live project runtimes/dolt servers (>= 1).
	MaxActiveRuntimes int `json:"max_active_runtimes"`
	// RuntimeIdleTimeoutSeconds is the idle-shutdown window in seconds (>= 1).
	RuntimeIdleTimeoutSeconds int `json:"runtime_idle_timeout_seconds"`
}

// Defaults returns the built-in configuration.
func Defaults() Settings {
	return Settings{
		MaxActiveRuntimes:         DefaultMaxActiveRuntimes,
		RuntimeIdleTimeoutSeconds: DefaultRuntimeIdleTimeoutSeconds,
	}
}

// normalized clamps out-of-range values back to defaults so a hand-edited or
// partial file can never disable the pool cap.
func (s Settings) normalized() Settings {
	if s.MaxActiveRuntimes < 1 {
		s.MaxActiveRuntimes = DefaultMaxActiveRuntimes
	}
	if s.RuntimeIdleTimeoutSeconds < 1 {
		s.RuntimeIdleTimeoutSeconds = DefaultRuntimeIdleTimeoutSeconds
	}
	return s
}

func path(root string) string {
	return filepath.Join(root, ".punakawan", "panel", "settings.json")
}

// Load reads the panel settings under root, returning the defaults when the
// file is absent or unreadable/invalid (never an error for those cases).
func Load(root string) Settings {
	data, err := os.ReadFile(path(root))
	if err != nil {
		return Defaults()
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return Defaults()
	}
	return s.normalized()
}

// Save atomically writes the (normalized) settings under root.
func Save(root string, s Settings) error {
	s = s.normalized()
	dir := filepath.Join(root, ".punakawan", "panel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("settings: create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: marshal: %w", err)
	}
	tmp := path(root) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("settings: write: %w", err)
	}
	if err := os.Rename(tmp, path(root)); err != nil {
		return fmt.Errorf("settings: rename: %w", err)
	}
	return nil
}
