package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsWhenAbsent(t *testing.T) {
	s := Load(t.TempDir())
	if s.MaxActiveRuntimes != DefaultMaxActiveRuntimes || s.RuntimeIdleTimeoutSeconds != DefaultRuntimeIdleTimeoutSeconds {
		t.Fatalf("absent file should yield defaults, got %+v", s)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, Settings{MaxActiveRuntimes: 8, RuntimeIdleTimeoutSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	got := Load(root)
	if got.MaxActiveRuntimes != 8 || got.RuntimeIdleTimeoutSeconds != 300 {
		t.Fatalf("round trip = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".punakawan", "panel", "settings.json")); err != nil {
		t.Fatalf("settings file not written: %v", err)
	}
}

func TestLoadClampsInvalid(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".punakawan", "panel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"max_active_runtimes":0,"runtime_idle_timeout_seconds":-5}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Load(root)
	if got.MaxActiveRuntimes != DefaultMaxActiveRuntimes || got.RuntimeIdleTimeoutSeconds != DefaultRuntimeIdleTimeoutSeconds {
		t.Fatalf("invalid values should clamp to defaults, got %+v", got)
	}
}
