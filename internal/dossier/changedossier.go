// changedossier.go implements the §32-39 Change Dossier store from
// punakawan-role-config-distinguished-improvements-plan.md Part IV. It is a
// distinct concern from this package's context-dossier builder (dossier.go):
// that builder assembles a transient KnowledgeRecord for a run, whereas this
// file persists and versions the durable ChangeDossier proof artifact for a
// change. They share the package only because both are "dossier" concepts
// rooted in the same workspace; there is no code coupling between them.
//
// The store mirrors internal/project and internal/roleconfig's stateless
// "keyed by a workspace root" model rather than being an *app.App field,
// because dossiers are project-scoped and are read from non-primary projects
// too. Every dossier lives under <root>/.punakawan/dossiers/<id>/ as a
// manifest.yaml (cheap listing), a current.yaml (the full ChangeDossier), an
// append-only claims.jsonl, per-record evidence/<id>.yaml files, and immutable
// versions/<n>.yaml snapshots. Reads never fail purely because a dossier is
// absent: a missing dossier is synthesized as an empty draft, exactly like
// project.Load synthesizes a zero-metadata project.

package dossier

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

const (
	// SupportedVersion is the only change-dossier schema version understood,
	// matching pkg/protocol's `punakawan.change-dossier/v1` version const. It
	// is stamped onto every dossier written through this package so the
	// generated UnmarshalJSON never rejects our own output.
	SupportedVersion = "punakawan.change-dossier/v1"

	dirName     = ".punakawan"
	dossiersDir = "dossiers"

	manifestFile = "manifest.yaml"
	currentFile  = "current.yaml"
	claimsFile   = "claims.jsonl"
	evidenceSub  = "evidence"
	versionsSub  = "versions"
)

// ErrDossierNotFound is available for callers that must distinguish an absent
// dossier; the read path (Get/List) never returns it, synthesizing an empty
// draft instead so callers on a fresh workspace need not special-case "no
// dossier yet".
var ErrDossierNotFound = errors.New("dossier: not found")

// changeManifest is the cheap, listing-oriented sidecar kept alongside the
// full current.yaml. It duplicates only the three fields a directory listing
// needs (id/title/status) so List does not have to parse every full dossier.
type changeManifest struct {
	ID     string `yaml:"id"`
	Title  string `yaml:"title"`
	Status string `yaml:"status"`
}

// Loaded is the full result of a Get: the dossier plus its sibling claim and
// evidence records, resolved from the append-only log and the evidence
// directory. Claims and evidence are returned inline (not just their ids)
// because every caller that wants a dossier wants its proof with it.
type Loaded struct {
	Dossier  protocol.ChangeDossier
	Claims   []protocol.DossierClaim
	Evidence []protocol.DossierEvidence
}

func dossiersRoot(root string) string   { return filepath.Join(root, dirName, dossiersDir) }
func dossierDir(root, id string) string { return filepath.Join(dossiersRoot(root), id) }
func manifestPath(root, id string) string {
	return filepath.Join(dossierDir(root, id), manifestFile)
}
func currentPath(root, id string) string { return filepath.Join(dossierDir(root, id), currentFile) }
func claimsPath(root, id string) string  { return filepath.Join(dossierDir(root, id), claimsFile) }
func evidencePath(root, id, evidenceID string) string {
	return filepath.Join(dossierDir(root, id), evidenceSub, evidenceID+".yaml")
}
func versionPath(root, id string, n int) string {
	return filepath.Join(dossierDir(root, id), versionsSub, fmt.Sprintf("%d.yaml", n))
}

// Create initializes a brand-new dossier: it stamps the schema version, fills
// created/updated timestamps and a default draft status when the caller left
// them empty, then writes manifest.yaml and current.yaml. It does NOT snapshot
// (there is no prior state to preserve) - that is Put's job. Create overwrites
// any file already at the id, treating "create" as "establish the initial
// state"; callers that must not clobber should List/Get first.
func Create(root string, d protocol.ChangeDossier) (protocol.ChangeDossier, error) {
	now := time.Now().UTC()
	d.Version = SupportedVersion
	if d.Status == "" {
		d.Status = protocol.ChangeDossierStatusDraft
	}
	if d.CreatedAt == nil {
		d.CreatedAt = &now
	}
	if d.UpdatedAt == nil {
		d.UpdatedAt = &now
	}
	if err := os.MkdirAll(dossierDir(root, d.Id), 0o755); err != nil {
		return protocol.ChangeDossier{}, fmt.Errorf("dossier: mkdir %s: %w", dossierDir(root, d.Id), err)
	}
	if err := writeCurrent(root, d); err != nil {
		return protocol.ChangeDossier{}, err
	}
	if err := writeChangeManifest(root, d); err != nil {
		return protocol.ChangeDossier{}, err
	}
	return d, nil
}

// Put persists an updated dossier as the new current.yaml. Before overwriting,
// the existing current.yaml (if any) is copied verbatim into the next
// versions/<n>.yaml so every status-advancing save leaves an immutable
// snapshot of the state it superseded, per §33/§39's versioned-history
// requirement. UpdatedAt is refreshed to now; the schema version is re-stamped
// so a hand-built dossier still round-trips.
func Put(root string, d protocol.ChangeDossier) (protocol.ChangeDossier, error) {
	now := time.Now().UTC()
	d.Version = SupportedVersion
	if d.Status == "" {
		d.Status = protocol.ChangeDossierStatusDraft
	}
	if d.CreatedAt == nil {
		d.CreatedAt = &now
	}
	d.UpdatedAt = &now

	if err := os.MkdirAll(dossierDir(root, d.Id), 0o755); err != nil {
		return protocol.ChangeDossier{}, fmt.Errorf("dossier: mkdir %s: %w", dossierDir(root, d.Id), err)
	}
	// Snapshot the pre-mutation current.yaml into versions/ before overwriting
	// it, so accepted states remain traceable. Snapshots are numbered
	// sequentially and are never rewritten.
	if existing, err := os.ReadFile(currentPath(root, d.Id)); err == nil {
		if serr := snapshotVersion(root, d.Id, existing); serr != nil {
			return protocol.ChangeDossier{}, serr
		}
	} else if !os.IsNotExist(err) {
		return protocol.ChangeDossier{}, fmt.Errorf("dossier: read %s: %w", currentPath(root, d.Id), err)
	}

	if err := writeCurrent(root, d); err != nil {
		return protocol.ChangeDossier{}, err
	}
	if err := writeChangeManifest(root, d); err != nil {
		return protocol.ChangeDossier{}, err
	}
	return d, nil
}

// Get returns the full dossier plus its claims and evidence. A dossier that
// has never been written is a normal, empty state, not an error (mirrors
// project.Load): the returned ChangeDossier is a synthesized draft carrying
// just the requested id, and Claims/Evidence are empty but non-nil.
func Get(root, id string) (Loaded, error) {
	d, err := readCurrent(root, id)
	if err != nil {
		return Loaded{}, err
	}
	claims, err := readClaims(root, id)
	if err != nil {
		return Loaded{}, err
	}
	ev, err := readEvidence(root, id)
	if err != nil {
		return Loaded{}, err
	}
	return Loaded{Dossier: d, Claims: claims, Evidence: ev}, nil
}

// List returns the dossier ids present in this workspace, sorted. A workspace
// that has never held a dossier (no dossiers directory yet) yields an empty
// slice, not an error. Non-directory entries are ignored.
func List(root string) ([]string, error) {
	entries, err := os.ReadDir(dossiersRoot(root))
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dossier: list dossiers: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// readCurrent reads current.yaml, synthesizing an empty draft when the file is
// absent so no read path fails purely because a dossier does not exist yet.
func readCurrent(root, id string) (protocol.ChangeDossier, error) {
	data, err := os.ReadFile(currentPath(root, id))
	if os.IsNotExist(err) {
		return synthesizeDossier(id), nil
	}
	if err != nil {
		return protocol.ChangeDossier{}, fmt.Errorf("dossier: read %s: %w", currentPath(root, id), err)
	}
	var d protocol.ChangeDossier
	if err := yaml.Unmarshal(data, &d); err != nil {
		return protocol.ChangeDossier{}, fmt.Errorf("dossier: parse %s: %w", currentPath(root, id), err)
	}
	return d, nil
}

// synthesizeDossier builds the default empty draft returned for a dossier id
// that has no current.yaml yet.
func synthesizeDossier(id string) protocol.ChangeDossier {
	return protocol.ChangeDossier{
		Version: SupportedVersion,
		Id:      id,
		Status:  protocol.ChangeDossierStatusDraft,
	}
}

func writeCurrent(root string, d protocol.ChangeDossier) error {
	out, err := yaml.Marshal(d)
	if err != nil {
		return fmt.Errorf("dossier: marshal current: %w", err)
	}
	return atomicWrite(currentPath(root, d.Id), out)
}

func writeChangeManifest(root string, d protocol.ChangeDossier) error {
	out, err := yaml.Marshal(changeManifest{ID: d.Id, Title: d.Title, Status: string(d.Status)})
	if err != nil {
		return fmt.Errorf("dossier: marshal manifest: %w", err)
	}
	return atomicWrite(manifestPath(root, d.Id), out)
}

// snapshotVersion writes data to the next sequential versions/<n>.yaml. It
// never overwrites an existing snapshot (O_EXCL): once a version is recorded
// its form is immutable, per §33's versioned history.
func snapshotVersion(root, id string, data []byte) error {
	dir := filepath.Join(dossierDir(root, id), versionsSub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("dossier: mkdir %s: %w", dir, err)
	}
	dst := versionPath(root, id, nextVersion(root, id))
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("dossier: snapshot %s: %w", dst, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("dossier: write snapshot %s: %w", dst, err)
	}
	return nil
}

// nextVersion returns one past the highest numbered versions/<n>.yaml, or 1
// when none exist. It keys off the maximum, never the count, so a gap in the
// sequence can never cause a collision with an existing snapshot.
func nextVersion(root, id string) int {
	entries, err := os.ReadDir(filepath.Join(dossierDir(root, id), versionsSub))
	if err != nil {
		return 1
	}
	max := 0
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".yaml")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(name); err == nil && n > max {
			max = n
		}
	}
	return max + 1
}

// atomicWrite writes data to a sibling temp file then renames it over path, so
// a reader never observes a partially written file. The parent directory is
// created as needed.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("dossier: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".dossier-*.tmp")
	if err != nil {
		return fmt.Errorf("dossier: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("dossier: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("dossier: close temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("dossier: rename temp over %s: %w", path, err)
	}
	return nil
}

// ptr returns a pointer to v. The generated protocol types model every
// optional scalar as a pointer, so building them from literals needs this
// throughout the store.
func ptr[T any](v T) *T { return &v }
