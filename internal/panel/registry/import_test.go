package registry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// writeLegacyRegistry writes entries as the pre-kernel workspaces.yaml shape
// at path.
func writeLegacyRegistry(t *testing.T, path string, entries []protocol.PanelWorkspaceRegistryEntry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	data, err := yaml.Marshal(legacyFile{Version: "1", Workspaces: entries})
	if err != nil {
		t.Fatalf("marshal legacy file: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
}

func TestImportLegacyPreservesHistoricalFields(t *testing.T) {
	s := openTest(t)
	dir := t.TempDir() // a real directory so the entry's path resolves

	legacyPath := filepath.Join(t.TempDir(), "workspaces.yaml")
	registered := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	// LastSeenAt is deliberately DISTINCT from RegisteredAt: a normal Register
	// call collapses both to "now", so this is the field the dedicated import
	// path must preserve independently.
	lastSeen := registered.Add(72 * time.Hour)
	writeLegacyRegistry(t, legacyPath, []protocol.PanelWorkspaceRegistryEntry{
		{
			Id:           "ws-1",
			Path:         dir,
			DisplayName:  strPtr("My Workspace"),
			RegisteredAt: registered,
			LastSeenAt:   &lastSeen,
			Pinned:       boolPtr(true),
		},
	})

	if warn := s.importLegacy(legacyPath); warn != nil {
		t.Fatalf("importLegacy: %v", warn)
	}

	got, err := s.Get("ws-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.RegisteredAt.Equal(registered) {
		t.Fatalf("RegisteredAt = %v, want %v", got.RegisteredAt, registered)
	}
	if got.LastSeenAt == nil || !got.LastSeenAt.Equal(lastSeen) {
		t.Fatalf("LastSeenAt = %v, want %v (must NOT collapse to RegisteredAt)", got.LastSeenAt, lastSeen)
	}
	if got.LastSeenAt.Equal(got.RegisteredAt) {
		t.Fatal("LastSeenAt collapsed to RegisteredAt - the whole point of importEntry over Register")
	}
	if got.DisplayName == nil || *got.DisplayName != "My Workspace" {
		t.Fatalf("DisplayName = %v, want My Workspace", got.DisplayName)
	}
	if got.Pinned == nil || !*got.Pinned {
		t.Fatalf("Pinned = %v, want true preserved", got.Pinned)
	}

	// Legacy file renamed to .imported and not deleted.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy path still exists after import (err=%v), want gone", err)
	}
	if _, err := os.Stat(legacyPath + ".imported"); err != nil {
		t.Fatalf(".imported backup missing after import: %v", err)
	}
}

func TestImportLegacySecondOpenDoesNotDuplicate(t *testing.T) {
	s := openTest(t)
	dir := t.TempDir()
	now := time.Now().UTC()
	legacyPath := filepath.Join(t.TempDir(), "workspaces.yaml")
	writeLegacyRegistry(t, legacyPath, []protocol.PanelWorkspaceRegistryEntry{
		{Id: "ws-1", Path: dir, RegisteredAt: now, LastSeenAt: &now},
	})

	if warn := s.importLegacy(legacyPath); warn != nil {
		t.Fatalf("first importLegacy: %v", warn)
	}
	// Simulate a restart: the original legacy file was renamed away, so the
	// second import hits ENOENT and is a correct no-op.
	if warn := s.importLegacy(legacyPath); warn != nil {
		t.Fatalf("second importLegacy: %v", warn)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("List = %d after two imports, want 1 (no duplication)", len(all))
	}
}

func TestImportLegacyNoFileIsNoOp(t *testing.T) {
	s := openTest(t)
	legacyPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	if warn := s.importLegacy(legacyPath); warn != nil {
		t.Fatalf("importLegacy with no legacy file = %v, want nil", warn)
	}
	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("List = %d, want 0 (no phantom import)", len(all))
	}
	if _, err := os.Stat(legacyPath + ".imported"); !os.IsNotExist(err) {
		t.Fatalf("unexpected .imported file created (err=%v)", err)
	}
}
