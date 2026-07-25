package artifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ErrPlanNotFound is returned by Current/Version when planID has no
// current.yaml pointer yet.
var ErrPlanNotFound = errors.New("artifact: plan not found")

// ErrVersionNotFound is returned by Version when the requested version
// number has no versions/<n>.md file.
var ErrVersionNotFound = errors.New("artifact: version not found")

// referenceDocument is §4's ArtifactReference YAML envelope
// (api_version/kind wrapping the artifact: mapping) - ArtifactReference
// itself is only the inner mapping, not the whole document.
type referenceDocument struct {
	APIVersion string                     `yaml:"api_version"`
	Kind       string                     `yaml:"kind"`
	Artifact   protocol.ArtifactReference `yaml:"artifact"`
}

// PlanStore implements §7's plan storage layout under
// <WorkspaceRoot>/.punakawan/plans/<plan-id>/: an append-only
// versions/<n>.md sequence plus a current.yaml pointer at the latest
// version. It is the only writer of plan content this package provides
// - there is no update-in-place path, per §10's "never overwrite a
// verified version in place."
type PlanStore struct {
	WorkspaceRoot string

	locksOnce sync.Once
	locks     *KeyedMutex
}

// LockArtifact serializes callers' multi-step read-check-write sequences
// against the same artifact id (e.g. AcceptProposalHandler's "read
// current version, compare against the proposal's base, and if equal
// create the next version" sequence, per §12's optimistic-concurrency
// rule). Without this, two concurrent accept calls for two different
// reviews both proposing against the same base version can each read
// Current() before either has written CreateVersion's next version,
// so both observe current==base and both succeed - exactly the "silently
// overwrite the newer version" outcome §12 forbids. It has no effect
// across different artifact ids. Call the returned func to release,
// typically via `defer plans.LockArtifact(id)()`.
func (s *PlanStore) LockArtifact(artifactID string) func() {
	s.locksOnce.Do(func() { s.locks = NewKeyedMutex() })
	return s.locks.Lock(artifactID)
}

func (s *PlanStore) planDir(planID string) string {
	return filepath.Join(s.WorkspaceRoot, ".punakawan", "plans", planID)
}

func (s *PlanStore) versionsDir(planID string) string {
	return filepath.Join(s.planDir(planID), "versions")
}

func (s *PlanStore) currentPath(planID string) string {
	return filepath.Join(s.planDir(planID), "current.yaml")
}

func (s *PlanStore) versionPath(planID string, version int) string {
	return filepath.Join(s.versionsDir(planID), fmt.Sprintf("%d.md", version))
}

func (s *PlanStore) versionCanonicalLocation(planID string, version int) string {
	return filepath.Join(".punakawan", "plans", planID, "versions", fmt.Sprintf("%d.md", version))
}

func (s *PlanStore) plansRoot() string {
	return filepath.Join(s.WorkspaceRoot, ".punakawan", "plans")
}

func (s *PlanStore) manifestPath(planID string) string {
	return filepath.Join(s.planDir(planID), "manifest.yaml")
}

// CreateVersion writes content as the next immutable version of planID
// (1 for a brand-new plan, latest+1 otherwise) and repoints current.yaml
// at it. It refuses to touch an existing version file - version numbers
// are assigned by this method, never chosen by the caller, so there is
// no path that could overwrite one.
func (s *PlanStore) CreateVersion(planID, workspaceID string, content []byte, now time.Time) (protocol.ArtifactReference, error) {
	latest, err := s.latestVersion(planID)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	version := latest + 1

	if err := os.MkdirAll(s.versionsDir(planID), 0o755); err != nil {
		return protocol.ArtifactReference{}, fmt.Errorf("artifact: create plan version dir: %w", err)
	}
	path := s.versionPath(planID, version)
	if _, err := os.Stat(path); err == nil {
		return protocol.ArtifactReference{}, fmt.Errorf("artifact: version %d of %q already exists - immutable versions are never overwritten", version, planID)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return protocol.ArtifactReference{}, fmt.Errorf("artifact: write plan version: %w", err)
	}

	location := s.versionCanonicalLocation(planID, version)
	ref := protocol.ArtifactReference{
		Type:              protocol.ArtifactReferenceTypePlan,
		Id:                planID,
		Version:           version,
		RevisionHash:      Hash(content),
		WorkspaceId:       workspaceID,
		Format:            protocol.ArtifactReferenceFormatMarkdown,
		CanonicalLocation: &location,
	}
	if err := s.writeCurrent(planID, ref); err != nil {
		return protocol.ArtifactReference{}, err
	}
	// Keep the manifest's current_version pointer in step with the version
	// just written, but ONLY when a manifest already exists. A plan that
	// was never given a manifest (the common case for the existing
	// review->proposal->accept flow, and every pre-Phase-7 test) is left
	// exactly as it was - CreateVersion never synthesizes a manifest, so
	// its behavior is unchanged when none is present and it never fails
	// purely because a plan has no manifest. Only status is left untouched
	// here: lifecycle transitions belong to the review/proposal handlers,
	// not to a raw version write.
	if err := s.bumpManifestVersion(planID, version); err != nil {
		return protocol.ArtifactReference{}, err
	}
	return ref, nil
}

// bumpManifestVersion updates an existing manifest's CurrentVersion to
// version. It is a no-op (nil error) when the plan has no manifest.yaml,
// which is why CreateVersion stays backward compatible for manifest-less
// plans.
func (s *PlanStore) bumpManifestVersion(planID string, version int) error {
	m, err := ReadManifest(s.manifestPath(planID))
	if errors.Is(err, ErrManifestNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if m.CurrentVersion == version {
		return nil
	}
	m.CurrentVersion = version
	return WriteManifest(s.manifestPath(planID), m)
}

// Current returns planID's current (latest) version reference.
func (s *PlanStore) Current(planID string) (protocol.ArtifactReference, error) {
	data, err := os.ReadFile(s.currentPath(planID))
	if os.IsNotExist(err) {
		return protocol.ArtifactReference{}, ErrPlanNotFound
	}
	if err != nil {
		return protocol.ArtifactReference{}, fmt.Errorf("artifact: read current.yaml: %w", err)
	}
	var doc referenceDocument
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return protocol.ArtifactReference{}, fmt.Errorf("artifact: parse current.yaml: %w", err)
	}
	return doc.Artifact, nil
}

// Version returns one specific version's content and reference. Older
// versions have no persisted sidecar reference of their own (§7's
// layout is just versions/<n>.md); RevisionHash/CanonicalLocation/
// Version are recomputed from the immutable file itself, and the
// remaining fields (Type/WorkspaceId/Format) are copied from Current,
// since those never vary across a plan's versions.
func (s *PlanStore) Version(planID string, version int) ([]byte, protocol.ArtifactReference, error) {
	content, err := os.ReadFile(s.versionPath(planID, version))
	if os.IsNotExist(err) {
		return nil, protocol.ArtifactReference{}, fmt.Errorf("artifact: %w: version %d of %q", ErrVersionNotFound, version, planID)
	}
	if err != nil {
		return nil, protocol.ArtifactReference{}, fmt.Errorf("artifact: read plan version: %w", err)
	}

	ref, err := s.Current(planID)
	if err != nil {
		return nil, protocol.ArtifactReference{}, err
	}
	location := s.versionCanonicalLocation(planID, version)
	ref.Version = version
	ref.RevisionHash = Hash(content)
	ref.CanonicalLocation = &location
	return content, ref, nil
}

func (s *PlanStore) writeCurrent(planID string, ref protocol.ArtifactReference) error {
	doc := referenceDocument{APIVersion: "punakawan.dev/v1", Kind: "ArtifactReference", Artifact: ref}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("artifact: marshal current.yaml: %w", err)
	}
	if err := os.WriteFile(s.currentPath(planID), data, 0o644); err != nil {
		return fmt.Errorf("artifact: write current.yaml: %w", err)
	}
	return nil
}

func (s *PlanStore) latestVersion(planID string) (int, error) {
	entries, err := os.ReadDir(s.versionsDir(planID))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("artifact: list plan versions: %w", err)
	}

	max := 0
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(name)
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max, nil
}

// ListPlans enumerates the plan ids in this workspace - one per
// subdirectory of .punakawan/plans, sorted. A workspace that has never
// held a plan (no plans directory yet) is a normal, empty state, not an
// error, so it returns an empty slice. Non-directory entries (stray
// files) are ignored.
func (s *PlanStore) ListPlans() ([]string, error) {
	entries, err := os.ReadDir(s.plansRoot())
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("artifact: list plans: %w", err)
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

// Manifest returns planID's manifest. When a manifest.yaml is present it
// is read verbatim. When it is absent - a plan can have versions and a
// current.yaml but no manifest, e.g. one created purely through the
// existing review/proposal flow - a minimal manifest is synthesized from
// current.yaml (id, current version, default draft status), mirroring
// project.Load's "a project with no project.yaml is normal, not an
// error" rule. Only a plan directory that does not exist at all yields
// ErrPlanNotFound.
func (s *PlanStore) Manifest(planID string) (*PlanManifest, error) {
	m, err := ReadManifest(s.manifestPath(planID))
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, ErrManifestNotFound) {
		return nil, err
	}
	if _, statErr := os.Stat(s.planDir(planID)); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil, fmt.Errorf("%w: %q", ErrPlanNotFound, planID)
		}
		return nil, fmt.Errorf("artifact: stat plan dir: %w", statErr)
	}
	return s.synthesizeManifest(planID), nil
}

// synthesizeManifest builds the default manifest for a plan directory
// that has no manifest.yaml yet. Title defaults to the id and status to
// draft; current_version is taken from current.yaml when present (0 when
// the plan has no versions yet).
func (s *PlanStore) synthesizeManifest(planID string) *PlanManifest {
	m := &PlanManifest{
		Version:      PlanManifestVersion,
		ID:           planID,
		Title:        planID,
		Status:       PlanStatusDraft,
		DerivedFrom:  Derivations{},
		RelatedTasks: []string{},
	}
	if ref, err := s.Current(planID); err == nil {
		m.CurrentVersion = ref.Version
	}
	return m
}

// SaveManifest writes planID's manifest, forcing the manifest's own id to
// match its storage location and defaulting an empty status to draft. A
// non-empty status that is not one of the five defined lifecycle values
// is rejected rather than persisted, so an out-of-vocabulary status can
// never reach disk through this guarded entry point.
func (s *PlanStore) SaveManifest(planID string, m *PlanManifest) error {
	if m == nil {
		return fmt.Errorf("artifact: save nil manifest for %q", planID)
	}
	m.ID = planID
	if m.Status == "" {
		m.Status = PlanStatusDraft
	}
	if !ValidPlanStatus(m.Status) {
		return fmt.Errorf("artifact: invalid plan status %q", m.Status)
	}
	return WriteManifest(s.manifestPath(planID), m)
}
