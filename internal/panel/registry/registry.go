// Package registry implements the global Punakawan Panel workspace
// registry: a small human-readable YAML file at an OS-specific config
// path, per punakawan-panel-implementation-plan.md §7. It stores panel
// discovery metadata only ("which local workspaces exist") - canonical
// workspace configuration always remains in each workspace's own
// .punakawan/workspace.yaml, and a path is never treated as valid solely
// because it appears here.
package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// version is the registry file's schema version tag, matching §7's
// "punakawan.workspace-registry/v1".
const version = "punakawan.workspace-registry/v1"

// ErrNotFound is returned by Get and Remove when no entry exists for the
// given id.
var ErrNotFound = errors.New("registry: workspace not found")

// ErrDuplicatePath is returned by Register when path (after resolving
// symlinks) already belongs to a different registered id, per §7's "
// duplicate physical paths are rejected."
var ErrDuplicatePath = errors.New("registry: path is already registered under a different id")

type file struct {
	Version    string                                 `yaml:"version"`
	Workspaces []protocol.PanelWorkspaceRegistryEntry `yaml:"workspaces"`
}

// Store reads and writes the registry file, serializing access with a
// mutex. Unlike this project's other stores (append-only JSONL), the
// registry is a small mutable list rewritten atomically on every change:
// there is no history to preserve, only "which workspaces currently
// exist."
type Store struct {
	path string
	mu   sync.Mutex
}

// pathOverrideEnv lets a caller redirect the registry to an explicit file,
// bypassing os.UserConfigDir(). This is a real feature (running multiple
// isolated `punakawan panel` instances side by side) as well as how tests
// avoid touching the developer's actual ~/.config/punakawan.
const pathOverrideEnv = "PUNAKAWAN_PANEL_REGISTRY_PATH"

// DefaultPath returns the registry file's OS-specific path, per §7:
// os.UserConfigDir() already resolves to ~/.config on Linux,
// ~/Library/Application Support on macOS, and %AppData% on Windows -
// exactly the roots §7 recommends. This implementation uses one
// lowercase "punakawan" directory name on every OS for consistency,
// rather than §7's per-OS example casing ("Punakawan" on macOS/Windows).
func DefaultPath() (string, error) {
	if override := os.Getenv(pathOverrideEnv); override != "" {
		return override, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("registry: resolve config dir: %w", err)
	}
	return filepath.Join(dir, "punakawan", "workspaces.yaml"), nil
}

// Open opens the registry at DefaultPath, creating an empty one if it
// does not exist yet.
func Open() (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return OpenAt(path)
}

// OpenAt opens the registry at an explicit path, mainly for tests.
func OpenAt(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("registry: create %s: %w", filepath.Dir(path), err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := writeFile(path, file{Version: version}); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("registry: stat %s: %w", path, err)
	}
	return &Store{path: path}, nil
}

func readFile(path string) (file, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return file{}, fmt.Errorf("registry: read %s: %w", path, err)
	}
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return file{}, fmt.Errorf("registry: decode %s: %w", path, err)
	}
	return f, nil
}

// writeFile persists f atomically: write to a temp file in the same
// directory, then rename over path, so a reader never observes a
// partially-written registry.
func writeFile(path string, f file) error {
	f.Version = version
	data, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("registry: encode %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".workspaces-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("registry: create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("registry: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("registry: close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("registry: rename into %s: %w", path, err)
	}
	return nil
}

// List returns every registered workspace entry.
func (s *Store) List() ([]protocol.PanelWorkspaceRegistryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := readFile(s.path)
	if err != nil {
		return nil, err
	}
	return f.Workspaces, nil
}

// Get returns the entry for id, or ErrNotFound.
func (s *Store) Get(id string) (protocol.PanelWorkspaceRegistryEntry, error) {
	entries, err := s.List()
	if err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, err
	}
	for _, e := range entries {
		if e.Id == id {
			return e, nil
		}
	}
	return protocol.PanelWorkspaceRegistryEntry{}, ErrNotFound
}

// Register adds a new entry for id at path, or - if id is already
// registered - re-registers it (updating path and last_seen_at), so
// auto-registration on every `punakawan panel` startup is idempotent
// rather than erroring on the second run. Renaming displayName does not
// change id, per §7's "renaming a display label does not change the
// stable workspace ID."
func (s *Store) Register(id, path, displayName string, now time.Time) (protocol.PanelWorkspaceRegistryEntry, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := readFile(s.path)
	if err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, err
	}

	for i, e := range f.Workspaces {
		if e.Id == id {
			f.Workspaces[i].Path = path
			f.Workspaces[i].LastSeenAt = &now
			if displayName != "" {
				f.Workspaces[i].DisplayName = &displayName
			}
			if err := writeFile(s.path, f); err != nil {
				return protocol.PanelWorkspaceRegistryEntry{}, err
			}
			return f.Workspaces[i], nil
		}
		if e.Id != id {
			existingResolved, err := resolvePath(e.Path)
			if err == nil && existingResolved == resolved {
				return protocol.PanelWorkspaceRegistryEntry{}, fmt.Errorf("%w: %q is already registered as %q", ErrDuplicatePath, path, e.Id)
			}
		}
	}

	entry := protocol.PanelWorkspaceRegistryEntry{
		Id:           id,
		Path:         path,
		RegisteredAt: now,
		LastSeenAt:   &now,
	}
	if displayName != "" {
		entry.DisplayName = &displayName
	}
	f.Workspaces = append(f.Workspaces, entry)
	if err := writeFile(s.path, f); err != nil {
		return protocol.PanelWorkspaceRegistryEntry{}, err
	}
	return entry, nil
}

// Remove deletes the entry for id.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := readFile(s.path)
	if err != nil {
		return err
	}

	out := make([]protocol.PanelWorkspaceRegistryEntry, 0, len(f.Workspaces))
	found := false
	for _, e := range f.Workspaces {
		if e.Id == id {
			found = true
			continue
		}
		out = append(out, e)
	}
	if !found {
		return ErrNotFound
	}
	f.Workspaces = out
	return writeFile(s.path, f)
}

// SetPinned sets id's pinned flag.
func (s *Store) SetPinned(id string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := readFile(s.path)
	if err != nil {
		return err
	}
	for i, e := range f.Workspaces {
		if e.Id == id {
			f.Workspaces[i].Pinned = &pinned
			return writeFile(s.path, f)
		}
	}
	return ErrNotFound
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
