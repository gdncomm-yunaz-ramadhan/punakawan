package workflowdef

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ErrNotFound is returned by Get/SetEnabled when no definition exists for an
// id.
var ErrNotFound = errors.New("workflowdef: definition not found")

// ErrRevisionConflict is returned by Save when the incoming definition's
// Revision does not match the live file's current Revision — the caller edited
// a stale copy and must rebase. This mirrors internal/project's optimistic
// concurrency so the panel can surface a 409.
var ErrRevisionConflict = errors.New("workflowdef: revision conflict")

// Store persists workflow Definitions one-file-per-id as YAML under
// <root>/.punakawan/workflows/. Prior revisions of a definition are snapshotted
// under workflows/versions/<id>/<oldRev>.yaml so definitions remain immutable
// by version even though the live file at <id>.yaml is mutable in place.
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open returns a Store rooted at <root>/.punakawan/workflows/, creating
// nothing: the directory appears when a definition is first saved, so a
// caller that only lists definitions leaves the project untouched.
func Open(root string) (*Store, error) {
	return &Store{dir: filepath.Join(root, ".punakawan", "workflows")}, nil
}

// filePath returns the live YAML path for a definition id. The id is sanity
// checked so it cannot escape the workflows directory via path separators.
func (s *Store) filePath(id string) (string, error) {
	if id == "" || strings.ContainsAny(id, "/\\") || id == "." || id == ".." {
		return "", fmt.Errorf("workflowdef: invalid definition id %q", id)
	}
	return filepath.Join(s.dir, id+".yaml"), nil
}

// List returns every definition, sorted by id. The versions/ subdirectory and
// any non-.yaml files are ignored.
func (s *Store) List() ([]Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("workflowdef: read %s: %w", s.dir, err)
	}

	var defs []Definition
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		def, err := s.readFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	return defs, nil
}

// Get returns the current definition for id, or ErrNotFound.
func (s *Store) Get(id string) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getLocked(id)
}

func (s *Store) getLocked(id string) (Definition, error) {
	path, err := s.filePath(id)
	if err != nil {
		return Definition{}, err
	}
	def, err := s.readFile(path)
	if os.IsNotExist(err) {
		return Definition{}, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if err != nil {
		return Definition{}, err
	}
	return def, nil
}

func (s *Store) readFile(path string) (Definition, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, err
	}
	var def Definition
	if err := yaml.Unmarshal(raw, &def); err != nil {
		return Definition{}, fmt.Errorf("workflowdef: decode %s: %w", path, err)
	}
	return def, nil
}

// Save writes def as the current definition for def.ID, bumping its Revision
// and snapshotting the prior file.
//
// Concurrency: for an existing definition, def.Revision must equal the live
// file's current Revision, else ErrRevisionConflict. For a new definition the
// incoming Revision is ignored. On success the live file is written with
// Revision incremented (starting at 1 for a new definition), and the prior
// content — if any — is copied to versions/<id>/<oldRev>.yaml first so old
// versions stay immutable. The returned Definition carries the persisted
// Revision.
func (s *Store) Save(def Definition) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.filePath(def.ID)
	if err != nil {
		return Definition{}, err
	}

	prior, err := s.readFile(path)
	switch {
	case os.IsNotExist(err):
		// New definition: first persisted revision is 1.
		def.Revision = 1
	case err != nil:
		return Definition{}, err
	default:
		if def.Revision != prior.Revision {
			return Definition{}, fmt.Errorf("%w: have %d, want %d", ErrRevisionConflict, def.Revision, prior.Revision)
		}
		if err := s.snapshot(def.ID, prior); err != nil {
			return Definition{}, err
		}
		def.Revision = prior.Revision + 1
	}

	if err := s.writeAtomic(path, def); err != nil {
		return Definition{}, err
	}
	return def, nil
}

// snapshot copies a prior revision to versions/<id>/<rev>.yaml so it remains
// available even after the live file is overwritten.
func (s *Store) snapshot(id string, prior Definition) error {
	dir := filepath.Join(s.dir, "versions", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workflowdef: create %s: %w", dir, err)
	}
	dst := filepath.Join(dir, fmt.Sprintf("%d.yaml", prior.Revision))
	return s.writeAtomic(dst, prior)
}

// writeAtomic marshals def to YAML and writes it to path via a temp file and
// rename, so a reader never observes a partially written definition.
func (s *Store) writeAtomic(path string, def Definition) error {
	raw, err := yaml.Marshal(def)
	if err != nil {
		return fmt.Errorf("workflowdef: encode %s: %w", def.ID, err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workflowdef: create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*.yaml")
	if err != nil {
		return fmt.Errorf("workflowdef: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("workflowdef: write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("workflowdef: close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("workflowdef: rename into %s: %w", path, err)
	}
	return nil
}

// SetEnabled flips a definition's Enabled flag, bumping its revision and
// snapshotting the prior version like any other edit. Returns ErrNotFound if
// no definition exists for id.
func (s *Store) SetEnabled(id string, enabled bool) (Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	def, err := s.getLocked(id)
	if err != nil {
		return Definition{}, err
	}
	if def.Enabled == enabled {
		return def, nil
	}

	path, _ := s.filePath(id)
	if err := s.snapshot(id, def); err != nil {
		return Definition{}, err
	}
	def.Enabled = enabled
	def.Revision++
	if err := s.writeAtomic(path, def); err != nil {
		return Definition{}, err
	}
	return def, nil
}
