package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// PlanManifestVersion is the schema version stamped into every
// manifest.yaml. It is embedded directly as the manifest's `version`
// field (there is no separate api_version/kind envelope like
// referenceDocument uses for current.yaml) - the manifest IS the
// document, not a reference wrapper around one.
const PlanManifestVersion = "punakawan.plan-manifest/v1"

// Plan lifecycle statuses (plan doc §9). A plan is born draft, becomes
// proposed when a review is opened against it, then accepted or rejected
// by that review's decision, and superseded once a newer plan replaces
// it. These are the only values SaveManifest will persist.
const (
	PlanStatusDraft      = "draft"
	PlanStatusProposed   = "proposed"
	PlanStatusAccepted   = "accepted"
	PlanStatusRejected   = "rejected"
	PlanStatusSuperseded = "superseded"
)

// ErrManifestNotFound is returned by ReadManifest when a plan directory
// has no manifest.yaml. Callers that treat a missing manifest as normal
// (PlanStore.Manifest synthesizes one, mirroring project.Load's "absent
// is a normal state" rule) check for it with errors.Is.
var ErrManifestNotFound = errors.New("artifact: plan manifest not found")

// ValidPlanStatus reports whether s is one of the five defined plan
// lifecycle statuses. An empty status is not valid here; SaveManifest
// defaults an empty status to draft before this check.
func ValidPlanStatus(s string) bool {
	switch s {
	case PlanStatusDraft, PlanStatusProposed, PlanStatusAccepted, PlanStatusRejected, PlanStatusSuperseded:
		return true
	default:
		return false
	}
}

// Derivations records what a plan was derived from (§9's derived_from):
// the knowledge records, workflow definitions, and project metadata keys
// that informed it. All three are optional; an omitted list marshals away
// rather than persisting an empty sequence.
type Derivations struct {
	Knowledge []string `yaml:"knowledge,omitempty" json:"knowledge,omitempty"`
	Workflows []string `yaml:"workflows,omitempty" json:"workflows,omitempty"`
	Metadata  []string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

// PlanManifest is §9's manifest.yaml: the small, human-editable index of
// a plan that sits beside its append-only versions/ sequence and
// current.yaml pointer under .punakawan/plans/<id>/. It carries the
// plan's title and lifecycle status, the pointer to its current version,
// what it was derived from, and the BD tasks it relates to - the fields
// a project-scoped plan listing needs without having to open every
// version file.
type PlanManifest struct {
	Version        string      `yaml:"version" json:"version"`
	ID             string      `yaml:"id" json:"id"`
	Title          string      `yaml:"title" json:"title"`
	Status         string      `yaml:"status" json:"status"`
	CurrentVersion int         `yaml:"current_version" json:"current_version"`
	DerivedFrom    Derivations `yaml:"derived_from" json:"derived_from"`
	RelatedTasks   []string    `yaml:"related_tasks,omitempty" json:"related_tasks,omitempty"`
}

// ReadManifest loads a PlanManifest from an exact manifest.yaml path. A
// missing file is reported as ErrManifestNotFound so callers can decide
// whether to synthesize one; every other read/parse failure is returned
// verbatim. An unrecognized schema version is rejected rather than
// silently reinterpreted, matching project.Load's version gate.
func ReadManifest(path string) (*PlanManifest, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, ErrManifestNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("artifact: read manifest %s: %w", path, err)
	}
	var m PlanManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("artifact: parse manifest %s: %w", path, err)
	}
	if m.Version != "" && m.Version != PlanManifestVersion {
		return nil, fmt.Errorf("artifact: unsupported plan manifest version %q (want %q)", m.Version, PlanManifestVersion)
	}
	if m.Version == "" {
		m.Version = PlanManifestVersion
	}
	if m.RelatedTasks == nil {
		m.RelatedTasks = []string{}
	}
	return &m, nil
}

// WriteManifest persists m to an exact manifest.yaml path via temp-file +
// rename, so a crash mid-write can never leave a half-written manifest
// (the same durability rule project.Save uses for project.yaml). It
// stamps the current schema version if the caller left it blank and
// creates the enclosing directory if needed. It does not validate the
// manifest's status - PlanStore.SaveManifest is the guarded entry point
// for that; WriteManifest is the low-level writer.
func WriteManifest(path string, m *PlanManifest) error {
	if m == nil {
		return fmt.Errorf("artifact: write nil manifest to %s", path)
	}
	if m.Version == "" {
		m.Version = PlanManifestVersion
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("artifact: marshal manifest: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("artifact: create manifest dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".manifest-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("artifact: create temp manifest: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("artifact: write temp manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("artifact: close temp manifest: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("artifact: commit manifest %s: %w", path, err)
	}
	return nil
}
