package registry

// This file holds the one-time, read-and-rename legacy-import facet only. It
// is NOT a persistence path: canonical registry state lives exclusively in the
// shared SQLite kernel (see registry.go). It touches the old pre-kernel YAML
// file (and honors the old path-override env var) solely to migrate any data
// registered before the upgrade, then leaves that file renamed in place as a
// backup. The storage-package consolidation guard deliberately excludes files
// named legacy_import.go for exactly this reason.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

// legacyImportSuffix marks the pre-kernel YAML registry file once it has
// been claimed for one-time import. After importing, the renamed file is
// deliberately left in place as a safety-net backup and is never deleted,
// so a later manual recovery is still possible and a second import run
// correctly no-ops (the original path no longer exists).
const legacyImportSuffix = ".imported"

// legacyPathOverrideEnv mirrors the env var the pre-kernel registry honored
// to redirect its YAML file away from os.UserConfigDir() (deleted in the
// migration onto the shared kernel). It is read here only so a leftover
// install that stored its workspaces at a custom path can still be imported.
const legacyPathOverrideEnv = "PUNAKAWAN_PANEL_REGISTRY_PATH"

// legacyFile is the minimal shape of the pre-kernel workspaces.yaml: just
// enough to locate and read the entries for a one-time import. The old
// file/version/migrate machinery is deliberately NOT resurrected.
type legacyFile struct {
	Version    string                                 `yaml:"version"`
	Workspaces []protocol.PanelWorkspaceRegistryEntry `yaml:"workspaces"`
}

// legacyRegistryPath resolves where the pre-kernel workspaces.yaml would
// live, honoring a fresh read of the legacy path-override env var for parity
// with how the registry located its file before the migration.
func legacyRegistryPath() (string, error) {
	if override := os.Getenv(legacyPathOverrideEnv); override != "" {
		return override, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("registry: resolve config dir: %w", err)
	}
	return filepath.Join(dir, "punakawan", "workspaces.yaml"), nil
}

// importLegacy performs the one-time, rename-based, concurrency-safe import
// of the pre-kernel YAML registry file. It renames legacyPath to
// legacyPath+".imported" - an atomic operation that gives this process
// exclusive ownership of the contents: a racing second process attempting
// the same rename gets ENOENT and skips, which is what makes this safe
// without a separate lock. It then inserts every entry directly via SQL
// (see importEntry for why Register cannot be used) and leaves the renamed
// file in place as a backup, never deleting it.
//
// It returns nil when there is nothing to import (no legacy file, or another
// process already claimed it). Any failure to claim, read, or decode the
// legacy file is returned as a non-fatal warning for the caller to log.
func (s *Store) importLegacy(legacyPath string) error {
	imported := legacyPath + legacyImportSuffix
	if err := os.Rename(legacyPath, imported); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("registry: claim legacy file for import: %w", err)
	}

	data, err := os.ReadFile(imported)
	if err != nil {
		return fmt.Errorf("registry: read imported legacy file %s: %w", imported, err)
	}
	var f legacyFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("registry: decode imported legacy file %s: %w", imported, err)
	}
	if len(f.Workspaces) == 0 {
		return nil
	}

	ctx := context.Background()
	return s.write(ctx, "import legacy panel workspaces", func(tx *sql.Tx) error {
		for _, e := range f.Workspaces {
			if err := s.importEntry(ctx, tx, e); err != nil {
				return fmt.Errorf("registry: import legacy workspace %q: %w", e.Id, err)
			}
		}
		return nil
	})
}

// importEntry inserts a full protocol.PanelWorkspaceRegistryEntry directly,
// preserving each field independently. Unlike Register - whose signature
// forces registered_at and last_seen_at to the same "now" and cannot carry a
// pre-existing Pinned - this is the only insert path that keeps a legacy
// entry's real historical registered_at/last_seen_at/pinned/display_name. It
// is used solely by importLegacy. ON CONFLICT(id) DO NOTHING keeps the import
// robust: an id already present in the kernel (post-migration authoritative
// data) is left untouched rather than aborting the whole one-time import.
func (s *Store) importEntry(ctx context.Context, tx *sql.Tx, e protocol.PanelWorkspaceRegistryEntry) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO panel_workspaces (id, path, display_name, registered_at, last_seen_at, pinned)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO NOTHING`,
		e.Id, e.Path, nullString(e.DisplayName), e.RegisteredAt.Format(timeLayout),
		nullTime(e.LastSeenAt), nullBool(e.Pinned))
	return err
}
