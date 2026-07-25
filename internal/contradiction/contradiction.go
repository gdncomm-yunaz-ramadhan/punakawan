// Package contradiction is the stateless store for a Punakawan project's
// Contradiction Ledger, per
// punakawan-role-config-distinguished-improvements-plan.md Part II §16-22. A
// contradiction records a disagreement between two or more sources about one
// subject (a config key, an API operation, a requirement, ...) so the four
// roles can triage and resolve it deliberately rather than silently picking a
// winner.
//
// Like internal/project and internal/roleconfig this package is stateless:
// every function is keyed by a workspace `root` string and reads/writes files
// under <root>/.punakawan/contradictions/, so it can serve the primary project
// and any non-primary project a request resolves to without carrying per-app
// state. Records live one-per-file under records/<id>.yaml (the full record)
// with a derived index.yaml holding lightweight summaries, so a caller can list
// the ledger without deserializing every full record. Writes are temp-file +
// rename so a crash mid-write can never leave a half-written record, and a
// missing directory is a normal empty ledger, never an error (mirrors
// internal/project.Load).
package contradiction

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

const (
	// Version is the only contradiction schema version written, matching
	// protocol/contradiction.schema.json's version const. Put stamps it on
	// every record so a reader can reject an incompatible future shape.
	Version = "punakawan.contradiction/v1"

	dirName   = ".punakawan"
	subDir    = "contradictions"
	recordDir = "records"
	indexFile = "index.yaml"
)

var (
	// ErrNotFound signals Get/mutators were asked for an id that has no record
	// on disk. Unlike a missing directory (a normal empty ledger), a missing
	// specific record is a caller error: you cannot synthesize a contradiction
	// that was never detected.
	ErrNotFound = errors.New("contradiction: record not found")

	// ErrIllegalTransition signals a lifecycle transition the §18 DAG forbids.
	// It is a sentinel so callers (and tests) can distinguish a rejected state
	// change from an I/O failure.
	ErrIllegalTransition = errors.New("contradiction: illegal status transition")
)

// contradictionsDir returns <root>/.punakawan/contradictions.
func contradictionsDir(root string) string {
	return filepath.Join(root, dirName, subDir)
}

// recordsDir returns <root>/.punakawan/contradictions/records.
func recordsDir(root string) string {
	return filepath.Join(contradictionsDir(root), recordDir)
}

// recordPath returns the full-record path for id.
func recordPath(root, id string) string {
	return filepath.Join(recordsDir(root), id+".yaml")
}

// List reads every full record under records/ and returns them. A ledger that
// has never had a contradiction detected is a normal empty state, not an error:
// an absent directory yields an empty slice (mirrors internal/project.Load's
// missing-file handling).
func List(root string) ([]protocol.Contradiction, error) {
	dir := recordsDir(root)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []protocol.Contradiction{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("contradiction: read dir %s: %w", dir, err)
	}

	out := make([]protocol.Contradiction, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, fmt.Errorf("contradiction: read %s: %w", path, rerr)
		}
		var c protocol.Contradiction
		if uerr := yaml.Unmarshal(data, &c); uerr != nil {
			return nil, fmt.Errorf("contradiction: parse %s: %w", path, uerr)
		}
		out = append(out, c)
	}
	return out, nil
}

// Get returns the full record for id, or ErrNotFound if it does not exist.
func Get(root, id string) (*protocol.Contradiction, error) {
	path := recordPath(root, id)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("contradiction: %q: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("contradiction: read %s: %w", path, err)
	}
	var c protocol.Contradiction
	if uerr := yaml.Unmarshal(data, &c); uerr != nil {
		return nil, fmt.Errorf("contradiction: parse %s: %w", path, uerr)
	}
	return &c, nil
}

// PutOptions carries injected context for one Put. Now is injected (not read
// from the wall clock) so tests can assert exact created_at/updated_at values;
// a zero Now defaults to time.Now().UTC().
type PutOptions struct {
	Now time.Time
}

// Put creates or updates the record for c.Id and refreshes index.yaml. It
// stamps the schema Version on every write, sets CreatedAt on the first write
// only (preserving the original on updates, even if the caller passed a record
// with a nil CreatedAt), and sets UpdatedAt on every write. The record file is
// written temp-file + rename so a reader never observes a partial record.
func Put(root string, c protocol.Contradiction, opts PutOptions) error {
	if c.Id == "" {
		return fmt.Errorf("contradiction: put record with empty id")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	c.Version = Version

	// Preserve the original CreatedAt across updates: if the caller-supplied
	// record has no CreatedAt, adopt the on-disk one (an update) or stamp now
	// (a first write). This keeps created_at stable regardless of whether the
	// caller round-tripped through Get first.
	if c.CreatedAt == nil {
		if existing, err := Get(root, c.Id); err == nil && existing.CreatedAt != nil {
			c.CreatedAt = existing.CreatedAt
		} else {
			c.CreatedAt = &now
		}
	}
	c.UpdatedAt = &now

	dir := recordsDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("contradiction: mkdir %s: %w", dir, err)
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("contradiction: marshal %q: %w", c.Id, err)
	}
	if err := atomicWrite(recordPath(root, c.Id), out); err != nil {
		return err
	}
	return refreshIndex(root)
}

// indexEntry is one lightweight summary line in index.yaml, per §16: enough to
// list and triage the ledger (which subject, how severe, what state) without
// deserializing every full record.
type indexEntry struct {
	ID        string                         `yaml:"id"`
	Severity  protocol.ContradictionSeverity `yaml:"severity"`
	Status    protocol.ContradictionStatus   `yaml:"status"`
	Subject   protocol.ContradictionSubject  `yaml:"subject"`
	UpdatedAt *time.Time                     `yaml:"updated_at,omitempty"`
}

// indexDocument is the on-disk shape of index.yaml.
type indexDocument struct {
	Version        string       `yaml:"version"`
	Contradictions []indexEntry `yaml:"contradictions"`
}

// refreshIndex rebuilds index.yaml from the current full records. The index is
// a derived cache, so it is regenerated wholesale after every Put rather than
// mutated in place - that keeps it consistent with the records even if a record
// file was edited or removed out of band.
func refreshIndex(root string) error {
	records, err := List(root)
	if err != nil {
		return err
	}
	doc := indexDocument{Version: Version, Contradictions: make([]indexEntry, 0, len(records))}
	for _, c := range records {
		doc.Contradictions = append(doc.Contradictions, indexEntry{
			ID:        c.Id,
			Severity:  c.Severity,
			Status:    c.Status,
			Subject:   c.Subject,
			UpdatedAt: c.UpdatedAt,
		})
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("contradiction: marshal index: %w", err)
	}
	if err := os.MkdirAll(contradictionsDir(root), 0o755); err != nil {
		return fmt.Errorf("contradiction: mkdir %s: %w", contradictionsDir(root), err)
	}
	return atomicWrite(filepath.Join(contradictionsDir(root), indexFile), out)
}

// atomicWrite writes data to a sibling temp file then renames it over path so a
// reader never observes a partially written file (same guarantee as
// internal/project.atomicWrite).
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".contradiction-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("contradiction: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("contradiction: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("contradiction: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("contradiction: rename temp over %s: %w", path, err)
	}
	return nil
}
