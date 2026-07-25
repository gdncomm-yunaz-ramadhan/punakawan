// Package project loads, persists, and versions a Punakawan project's
// identity and generic metadata, per
// punakawan-panel-project-performance-improvement-plan.md §3/§4/§15. A
// project shares its id with the workspace it is rooted in (project id ==
// registry workspace id); its canonical file lives at
// <workspaceRoot>/.punakawan/project.yaml. Metadata is intentionally
// generic (key/description/value) so the same shape serves humans and
// agents without per-field code, per §4.
package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/workspace"
	"gopkg.in/yaml.v3"
)

const (
	// SupportedVersion is the only project.yaml schema version understood,
	// per §4.2's `version: punakawan.project/v1`.
	SupportedVersion = "punakawan.project/v1"

	dirName     = ".punakawan"
	configFile  = "project.yaml"
	subDir      = "project"
	versionsDir = "versions"
	auditFile   = "audit.jsonl"

	// DefaultActor is the audit actor recorded when a caller does not
	// specify one - mutations arriving through the panel are attributed to
	// "panel" since the local single-user panel has no per-request identity.
	DefaultActor = "panel"
)

// MetadataEntry is one generic project metadata item, per §4's required
// shape. Value may be a string, number, bool, []string, or a structured
// map/slice; validation (validation.go) rejects functions, other unsupported
// kinds, and secret-looking keys. JSON and YAML tags are load-bearing: the
// HTTP layer marshals this type directly into API responses and it is
// persisted verbatim into project.yaml.
type MetadataEntry struct {
	Key         string `json:"key" yaml:"key"`
	Description string `json:"description" yaml:"description"`
	Value       any    `json:"value" yaml:"value"`
}

// Project is a project's identity plus its ordered metadata and the current
// immutable revision. Path is the workspace root that contains .punakawan/;
// it is runtime-only (never serialized into project.yaml, whose location
// already implies it).
type Project struct {
	ID          string          `json:"id" yaml:"id"`
	Name        string          `json:"name" yaml:"name"`
	Description string          `json:"description" yaml:"description"`
	Path        string          `json:"path" yaml:"-"`
	Revision    int             `json:"revision" yaml:"revision"`
	Metadata    []MetadataEntry `json:"metadata" yaml:"metadata"`
}

// projectFile is the on-disk YAML shape. It is kept separate from Project so
// Project itself stays free of the persistence-only version field and so the
// runtime-only Path never leaks into the file.
type projectFile struct {
	Version     string          `yaml:"version"`
	ID          string          `yaml:"id"`
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Revision    int             `yaml:"revision"`
	Metadata    []MetadataEntry `yaml:"metadata"`
}

// configPath returns <root>/.punakawan/project.yaml.
func configPath(root string) string {
	return filepath.Join(root, dirName, configFile)
}

// Load reads <root>/.punakawan/project.yaml. If the file is absent it
// synthesizes a zero-metadata project (revision 0) named after the workspace
// rooted at root, per §3: a project that has never had metadata edited is a
// normal state, not an error. The workspace name is resolved through
// internal/workspace (Discover handles both an explicit workspace.yaml and
// the implicit single-repo fallback); if even that cannot be resolved, the
// root's base name is used so Load still never fails purely because
// project.yaml is missing.
func Load(root string) (*Project, error) {
	path := configPath(root)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return synthesize(root), nil
	}
	if err != nil {
		return nil, fmt.Errorf("project: read %s: %w", path, err)
	}

	var pf projectFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("project: parse %s: %w", path, err)
	}
	if pf.Version != SupportedVersion {
		return nil, fmt.Errorf("project: unsupported version %q (want %q)", pf.Version, SupportedVersion)
	}

	p := &Project{
		ID:          pf.ID,
		Name:        pf.Name,
		Description: pf.Description,
		Path:        root,
		Revision:    pf.Revision,
		Metadata:    pf.Metadata,
	}
	if p.Metadata == nil {
		p.Metadata = []MetadataEntry{}
	}
	return p, nil
}

// synthesize builds the default zero-metadata project for a root with no
// project.yaml yet.
func synthesize(root string) *Project {
	id := filepath.Base(root)
	name := id
	if ws, err := workspace.Discover(root); err == nil {
		if ws.ID != "" {
			id = ws.ID
		}
		if ws.Name != "" {
			name = ws.Name
		}
	}
	return &Project{
		ID:       id,
		Name:     name,
		Path:     root,
		Revision: 0,
		Metadata: []MetadataEntry{},
	}
}

// SaveOptions carries the audit context for one Save. Now and Actor are
// injected (not read from the wall clock or an ambient identity) so tests can
// assert exact audit lines; an empty Actor defaults to DefaultActor and a
// zero Now defaults to time.Now().UTC().
type SaveOptions struct {
	Now    time.Time
	Actor  string
	Action string // "add" | "update" | "delete" (free-form; recorded verbatim)
	Key    string // metadata key the action touched, if any
}

// auditRecord is one line appended to project/audit.jsonl per accepted
// mutation, per §15's "actor, timestamp, ... optimistic base revision".
type auditRecord struct {
	Ts          time.Time `json:"ts"`
	Actor       string    `json:"actor"`
	Action      string    `json:"action"`
	Key         string    `json:"key,omitempty"`
	OldRevision int       `json:"old_revision"`
	NewRevision int       `json:"new_revision"`
}

// Save atomically persists p to <root>/.punakawan/project.yaml. Before
// overwriting, the current on-disk file (if any) is snapshotted, immutably,
// to <root>/.punakawan/project/versions/<oldRevision>.yaml so accepted
// versions remain traceable (§15), and an audit line is appended to
// <root>/.punakawan/project/audit.jsonl. The write itself is temp-file +
// rename so a crash mid-write can never leave a half-written project.yaml.
func Save(root string, p *Project, opts SaveOptions) error {
	if p == nil {
		return fmt.Errorf("project: save nil project")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	actor := opts.Actor
	if actor == "" {
		actor = DefaultActor
	}

	baseDir := filepath.Join(root, dirName)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("project: mkdir %s: %w", baseDir, err)
	}
	path := configPath(root)

	// Snapshot the pre-mutation file (if present) before we overwrite it, so
	// history is captured exactly as it was accepted. Version files are
	// immutable: an existing snapshot for that revision is never rewritten.
	oldRevision := 0
	if existing, err := os.ReadFile(path); err == nil {
		var prev projectFile
		if uerr := yaml.Unmarshal(existing, &prev); uerr == nil {
			oldRevision = prev.Revision
		}
		if serr := snapshotVersion(root, oldRevision, existing); serr != nil {
			return serr
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("project: read %s: %w", path, err)
	}

	pf := projectFile{
		Version:     SupportedVersion,
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Revision:    p.Revision,
		Metadata:    p.Metadata,
	}
	if pf.Metadata == nil {
		pf.Metadata = []MetadataEntry{}
	}
	out, err := yaml.Marshal(pf)
	if err != nil {
		return fmt.Errorf("project: marshal: %w", err)
	}
	if err := atomicWrite(path, out); err != nil {
		return err
	}

	return appendAudit(root, auditRecord{
		Ts:          now,
		Actor:       actor,
		Action:      opts.Action,
		Key:         opts.Key,
		OldRevision: oldRevision,
		NewRevision: p.Revision,
	})
}

// snapshotVersion writes the pre-mutation bytes to
// project/versions/<rev>.yaml, creating the directory as needed. It never
// overwrites an existing snapshot (O_EXCL): once a revision is accepted its
// recorded form is immutable.
func snapshotVersion(root string, rev int, data []byte) error {
	dir := filepath.Join(root, dirName, subDir, versionsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("project: mkdir %s: %w", dir, err)
	}
	dst := filepath.Join(dir, fmt.Sprintf("%d.yaml", rev))
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		return nil // immutable: already snapshotted
	}
	if err != nil {
		return fmt.Errorf("project: snapshot %s: %w", dst, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("project: write snapshot %s: %w", dst, err)
	}
	return nil
}

// appendAudit appends one JSON line to project/audit.jsonl.
func appendAudit(root string, rec auditRecord) error {
	dir := filepath.Join(root, dirName, subDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("project: mkdir %s: %w", dir, err)
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("project: marshal audit: %w", err)
	}
	path := filepath.Join(dir, auditFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("project: open audit %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("project: append audit: %w", err)
	}
	return nil
}

// atomicWrite writes data to a sibling temp file then renames it over path,
// so a reader never observes a partially written file.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".project-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("project: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("project: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("project: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("project: rename temp over %s: %w", path, err)
	}
	return nil
}

// findMetadata returns the index of the entry whose key matches key
// case-insensitively, or -1. Keys are compared case-insensitively per §4.1.
func (p *Project) findMetadata(key string) int {
	target := strings.ToLower(strings.TrimSpace(key))
	for i, e := range p.Metadata {
		if strings.ToLower(strings.TrimSpace(e.Key)) == target {
			return i
		}
	}
	return -1
}

// AddMetadata appends entry after validating it and enforcing optimistic
// locking: baseRevision must equal the project's current Revision or
// ErrRevisionConflict is returned and nothing is mutated. On success Revision
// is bumped by one; the caller then persists with Save.
func (p *Project) AddMetadata(entry MetadataEntry, baseRevision int) error {
	if baseRevision != p.Revision {
		return revisionConflict(baseRevision, p.Revision)
	}
	if err := validateEntry(entry); err != nil {
		return err
	}
	if p.findMetadata(entry.Key) >= 0 {
		return fmt.Errorf("project: key %q already exists: %w", entry.Key, ErrDuplicateKey)
	}
	p.Metadata = append(p.Metadata, entry)
	p.Revision++
	return nil
}

// UpdateMetadata mutates the entry identified by key. A nil newDescription
// leaves the description unchanged; a nil newValue leaves the value unchanged
// (a metadata value is never legitimately a bare nil - value is required - so
// nil unambiguously means "not supplied" here). The resulting entry is
// re-validated. Optimistic locking is enforced as in AddMetadata; an unknown
// key yields ErrKeyNotFound.
func (p *Project) UpdateMetadata(key string, newDescription *string, newValue any, baseRevision int) error {
	if baseRevision != p.Revision {
		return revisionConflict(baseRevision, p.Revision)
	}
	idx := p.findMetadata(key)
	if idx < 0 {
		return fmt.Errorf("project: unknown metadata key %q: %w", key, ErrKeyNotFound)
	}
	updated := p.Metadata[idx]
	if newDescription != nil {
		updated.Description = *newDescription
	}
	if newValue != nil {
		updated.Value = newValue
	}
	if err := validateEntry(updated); err != nil {
		return err
	}
	p.Metadata[idx] = updated
	p.Revision++
	return nil
}

// DeleteMetadata removes the entry identified by key. Optimistic locking is
// enforced as in AddMetadata; an unknown key yields ErrKeyNotFound.
func (p *Project) DeleteMetadata(key string, baseRevision int) error {
	if baseRevision != p.Revision {
		return revisionConflict(baseRevision, p.Revision)
	}
	idx := p.findMetadata(key)
	if idx < 0 {
		return fmt.Errorf("project: unknown metadata key %q: %w", key, ErrKeyNotFound)
	}
	p.Metadata = append(p.Metadata[:idx], p.Metadata[idx+1:]...)
	p.Revision++
	return nil
}

// Metadata returns the entry for key (case-insensitive) and whether it exists.
func (p *Project) MetadataFor(key string) (MetadataEntry, bool) {
	if idx := p.findMetadata(key); idx >= 0 {
		return p.Metadata[idx], true
	}
	return MetadataEntry{}, false
}

func revisionConflict(base, current int) error {
	return fmt.Errorf("project: base revision %d does not match current revision %d: %w", base, current, ErrRevisionConflict)
}
