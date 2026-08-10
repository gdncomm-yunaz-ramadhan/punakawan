// Package registry implements the global Punakawan Panel workspace
// registry: panel discovery metadata ("which local workspace checkouts
// does this machine's panel know about"), persisted in the shared SQLite
// storage kernel (internal/storage, punokawan-14yn.16). Canonical
// workspace configuration always remains in each workspace's own
// .punakawan/workspace.yaml, and a path is never treated as valid solely
// because it appears here.
//
// The registry is genuinely machine-global, not project-scoped (there is
// exactly one per machine, like internal/procreg's process registry), so
// a Store carries no project id. It is also mutable-in-place: Register
// upserts by id, Remove deletes, SetPinned toggles a flag - there is no
// append-only history to fold, only "which workspaces currently exist."
package registry

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// timeLayout is how timestamps are stored in and parsed from the text
// columns, matching the rest of the storage kernel (see internal/procreg).
const timeLayout = time.RFC3339Nano

// ErrNotFound is returned by Get, Remove, and SetPinned when no entry
// exists for the given id.
var ErrNotFound = errors.New("registry: workspace not found")

// ErrDuplicatePath is returned by Register when path (after resolving
// symlinks) already belongs to a different registered id, per §7's
// "duplicate physical paths are rejected."
var ErrDuplicatePath = errors.New("registry: path is already registered under a different id")

// Store reads and writes panel workspace entries through the shared
// storage kernel. Schema migration happens once, centrally, when the
// kernel opens (internal/storage/migrations/0012_panel_registry.sql) - a
// Store never creates its own tables.
type Store struct {
	db *storage.DB
}

// New wraps an already-opened storage kernel database. Unlike the
// project-scoped stores (approvals/learning/syncqueue), it takes no
// project id: the panel registry is machine-global.
func New(db *storage.DB) *Store { return &Store{db: db} }

// Open opens a standalone connection to this machine's shared storage
// kernel (respecting PUNAKAWAN_DATA_DIR) and returns a Store over it. The
// CLI commands that use the registry call this independently of any
// *app.App they may also have loaded; WAL mode tolerates a second
// connection to the same file. Callers must Close the returned Store.
func Open() (*Store, error) {
	path, err := storage.DBPath()
	if err != nil {
		return nil, err
	}
	if err := storage.CheckLocation(path); err != nil {
		return nil, err
	}
	db, err := storage.Open(context.Background(), path)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close releases the underlying storage connection. It is a no-op-safe
// counterpart to Open; a Store built via New (sharing a caller-owned db)
// should not be Closed here - close the db through whoever opened it.
func (s *Store) Close() error { return s.db.Close() }

// writeKey returns a fresh random idempotency key. Every registry
// mutation must always take effect - re-registering an id updates its
// last_seen_at, pinning then unpinning must both apply - so a stable
// per-id key would let the kernel's replay dedup wrongly collapse the
// second write. A unique key per call is the right choice here (matching
// the append-only stores), not the deterministic key internal/procreg
// uses, precisely because those operations recur while a procreg run is
// registered exactly once.
func writeKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("registry: generate write key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// List returns every registered workspace entry, in insertion order.
func (s *Store) List() ([]protocol.PanelWorkspaceRegistryEntry, error) {
	return s.all(context.Background())
}

func (s *Store) all(ctx context.Context) ([]protocol.PanelWorkspaceRegistryEntry, error) {
	rows, err := s.db.Reader().QueryContext(ctx,
		`SELECT id, path, display_name, registered_at, last_seen_at, pinned
		 FROM panel_workspaces ORDER BY seq ASC`)
	if err != nil {
		return nil, fmt.Errorf("registry: list: %w", err)
	}
	defer rows.Close()

	var out []protocol.PanelWorkspaceRegistryEntry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Get returns the entry for id, or ErrNotFound.
func (s *Store) Get(id string) (protocol.PanelWorkspaceRegistryEntry, error) {
	ctx := context.Background()
	row := s.db.Reader().QueryRowContext(ctx,
		`SELECT id, path, display_name, registered_at, last_seen_at, pinned
		 FROM panel_workspaces WHERE id = ?`, id)
	e, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.PanelWorkspaceRegistryEntry{}, ErrNotFound
	}
	if err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, fmt.Errorf("registry: get %s: %w", id, err)
	}
	return e, nil
}

func scanEntry(row interface {
	Scan(dest ...interface{}) error
}) (protocol.PanelWorkspaceRegistryEntry, error) {
	var e protocol.PanelWorkspaceRegistryEntry
	var displayName, lastSeen sql.NullString
	var registeredAt string
	var pinned sql.NullBool
	if err := row.Scan(&e.Id, &e.Path, &displayName, &registeredAt, &lastSeen, &pinned); err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, err
	}
	ra, err := time.Parse(timeLayout, registeredAt)
	if err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, fmt.Errorf("registry: parse registered_at for %s: %w", e.Id, err)
	}
	e.RegisteredAt = ra
	if displayName.Valid {
		dn := displayName.String
		e.DisplayName = &dn
	}
	if lastSeen.Valid {
		ls, err := time.Parse(timeLayout, lastSeen.String)
		if err != nil {
			return protocol.PanelWorkspaceRegistryEntry{}, fmt.Errorf("registry: parse last_seen_at for %s: %w", e.Id, err)
		}
		e.LastSeenAt = &ls
	}
	if pinned.Valid {
		p := pinned.Bool
		e.Pinned = &p
	}
	return e, nil
}

// Register adds a new entry for id at path, or - if id is already
// registered - re-registers it (updating path and last_seen_at, and
// display_name only when a non-empty one is given), so auto-registration
// on every `punakawan panel` startup is idempotent rather than erroring
// on the second run. Renaming displayName does not change id, per §7's
// "renaming a display label does not change the stable workspace ID."
//
// A brand-new id is rejected with ErrDuplicatePath if any OTHER entry
// already resolves (symlinks followed) to the same physical directory.
// Re-registration of an existing id skips that check: it keeps its own
// path, so it can never collide with itself.
func (s *Store) Register(id, path, displayName string, now time.Time) (protocol.PanelWorkspaceRegistryEntry, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, err
	}

	ctx := context.Background()
	entries, err := s.all(ctx)
	if err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, err
	}

	for _, e := range entries {
		if e.Id == id {
			updated := e
			updated.Path = path
			updated.LastSeenAt = &now
			if displayName != "" {
				dn := displayName
				updated.DisplayName = &dn
			}
			if err := s.write(ctx, "update panel workspace "+id, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx,
					`UPDATE panel_workspaces SET path = ?, display_name = ?, last_seen_at = ? WHERE id = ?`,
					updated.Path, nullString(updated.DisplayName), nullTime(updated.LastSeenAt), id)
				return err
			}); err != nil {
				return protocol.PanelWorkspaceRegistryEntry{}, err
			}
			return updated, nil
		}
	}

	for _, e := range entries {
		existingResolved, err := resolvePath(e.Path)
		if err == nil && existingResolved == resolved {
			return protocol.PanelWorkspaceRegistryEntry{}, fmt.Errorf("%w: %q is already registered as %q", ErrDuplicatePath, path, e.Id)
		}
	}

	entry := protocol.PanelWorkspaceRegistryEntry{
		Id:           id,
		Path:         path,
		RegisteredAt: now,
		LastSeenAt:   &now,
	}
	if displayName != "" {
		dn := displayName
		entry.DisplayName = &dn
	}
	if err := s.write(ctx, "register panel workspace "+id, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO panel_workspaces (id, path, display_name, registered_at, last_seen_at, pinned)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			entry.Id, entry.Path, nullString(entry.DisplayName), entry.RegisteredAt.Format(timeLayout),
			nullTime(entry.LastSeenAt), nullBool(entry.Pinned))
		return err
	}); err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, err
	}
	return entry, nil
}

// Remove deletes the entry for id, or returns ErrNotFound.
func (s *Store) Remove(id string) error {
	ctx := context.Background()
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.write(ctx, "remove panel workspace "+id, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM panel_workspaces WHERE id = ?`, id)
		return err
	})
}

// SetPinned sets id's pinned flag, or returns ErrNotFound.
func (s *Store) SetPinned(id string, pinned bool) error {
	ctx := context.Background()
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.write(ctx, fmt.Sprintf("set panel workspace %s pinned=%t", id, pinned), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE panel_workspaces SET pinned = ? WHERE id = ?`, pinned, id)
		return err
	})
}

// write runs fn as one audited kernel write under a fresh idempotency key.
func (s *Store) write(ctx context.Context, summary string, fn func(tx *sql.Tx) error) error {
	key, err := writeKey()
	if err != nil {
		return err
	}
	return s.db.Write(ctx, key, summary, fn)
}

func nullString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(timeLayout)
}

func nullBool(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

// resolvePath validates that path exists and is a directory, and returns
// its symlink-resolved absolute form for duplicate-path comparison.
func resolvePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("registry: resolve %s: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("registry: %s does not exist: %w", path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("registry: stat %s: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("registry: %s is not a directory", resolved)
	}
	return resolved, nil
}
