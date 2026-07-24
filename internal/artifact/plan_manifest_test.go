package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadManifestRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	want := &PlanManifest{
		ID:             "plan-panel",
		Title:          "Panel Plan",
		Status:         PlanStatusProposed,
		CurrentVersion: 3,
		DerivedFrom: Derivations{
			Knowledge: []string{"know-1"},
			Workflows: []string{"wf-a"},
			Metadata:  []string{"jira.project_key"},
		},
		RelatedTasks: []string{"punokawan-sv8"},
	}
	if err := WriteManifest(path, want); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if want.Version != PlanManifestVersion {
		t.Fatalf("WriteManifest did not stamp version, got %q", want.Version)
	}

	got, err := ReadManifest(path)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.Version != PlanManifestVersion {
		t.Fatalf("Version = %q, want %q", got.Version, PlanManifestVersion)
	}
	if got.ID != "plan-panel" || got.Title != "Panel Plan" || got.Status != PlanStatusProposed || got.CurrentVersion != 3 {
		t.Fatalf("scalar fields round-tripped wrong: %+v", got)
	}
	if len(got.DerivedFrom.Knowledge) != 1 || got.DerivedFrom.Knowledge[0] != "know-1" {
		t.Fatalf("DerivedFrom.Knowledge = %v", got.DerivedFrom.Knowledge)
	}
	if len(got.RelatedTasks) != 1 || got.RelatedTasks[0] != "punokawan-sv8" {
		t.Fatalf("RelatedTasks = %v", got.RelatedTasks)
	}
}

func TestReadManifestMissingIsSentinel(t *testing.T) {
	_, err := ReadManifest(filepath.Join(t.TempDir(), "manifest.yaml"))
	if !errors.Is(err, ErrManifestNotFound) {
		t.Fatalf("err = %v, want ErrManifestNotFound", err)
	}
}

func TestReadManifestRejectsUnknownVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte("version: punakawan.plan-manifest/v999\nid: x\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := ReadManifest(path); err == nil {
		t.Fatal("expected an error for an unsupported manifest version")
	}
}

func TestWriteManifestIsAtomicAndDefaultsVersion(t *testing.T) {
	dir := t.TempDir()
	// Path in a not-yet-existing subdir to prove WriteManifest creates it.
	path := filepath.Join(dir, "nested", "manifest.yaml")
	if err := WriteManifest(path, &PlanManifest{ID: "p", Status: PlanStatusDraft}); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	// No leftover temp files beside the manifest.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != "manifest.yaml" {
			t.Fatalf("stray file left after atomic write: %q", e.Name())
		}
	}
}

func TestManifestSynthesizedWhenAbsent(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}
	// A plan with versions/current.yaml but no manifest.yaml.
	if _, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v1"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	if _, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v2"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	m, err := s.Manifest("plan-panel")
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if m.ID != "plan-panel" || m.Title != "plan-panel" {
		t.Fatalf("synthesized id/title = %q/%q, want plan-panel", m.ID, m.Title)
	}
	if m.Status != PlanStatusDraft {
		t.Fatalf("synthesized status = %q, want draft", m.Status)
	}
	if m.CurrentVersion != 2 {
		t.Fatalf("synthesized current_version = %d, want 2 (from current.yaml)", m.CurrentVersion)
	}
	// Synthesizing must not have written a manifest file.
	if _, err := os.Stat(s.manifestPath("plan-panel")); !os.IsNotExist(err) {
		t.Fatalf("synthesize wrote a manifest to disk (stat err = %v)", err)
	}
}

func TestManifestUnknownPlanIsNotFound(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}
	if _, err := s.Manifest("no-such-plan"); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("err = %v, want ErrPlanNotFound", err)
	}
}

func TestSaveManifestGuardsStatus(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}

	// Empty status defaults to draft and the id is forced to the location.
	if err := s.SaveManifest("plan-x", &PlanManifest{Title: "X"}); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	m, err := s.Manifest("plan-x")
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if m.ID != "plan-x" {
		t.Fatalf("id = %q, want plan-x (forced to location)", m.ID)
	}
	if m.Status != PlanStatusDraft {
		t.Fatalf("status = %q, want draft default", m.Status)
	}

	// An out-of-vocabulary status is rejected before hitting disk.
	if err := s.SaveManifest("plan-y", &PlanManifest{Status: "bogus"}); err == nil {
		t.Fatal("expected SaveManifest to reject an invalid status")
	}
	if _, err := os.Stat(s.manifestPath("plan-y")); !os.IsNotExist(err) {
		t.Fatalf("invalid-status manifest reached disk (stat err = %v)", err)
	}
}

func TestListPlans(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}

	// Empty workspace: no plans dir yet is a normal empty result.
	ids, err := s.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans (empty): %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("ListPlans (empty) = %v, want []", ids)
	}

	for _, id := range []string{"plan-c", "plan-a", "plan-b"} {
		if _, err := s.CreateVersion(id, "punakawan", []byte("# x"), time.Now()); err != nil {
			t.Fatalf("CreateVersion(%s): %v", id, err)
		}
	}
	// A stray non-directory file under plans/ must be ignored.
	if err := os.WriteFile(filepath.Join(s.plansRoot(), "README"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed stray file: %v", err)
	}

	ids, err = s.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	want := []string{"plan-a", "plan-b", "plan-c"}
	if len(ids) != len(want) {
		t.Fatalf("ListPlans = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ListPlans = %v, want sorted %v", ids, want)
		}
	}
}

func TestCreateVersionBumpsExistingManifestOnly(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}

	// With a manifest present, CreateVersion advances current_version.
	if err := s.SaveManifest("plan-m", &PlanManifest{Title: "M", Status: PlanStatusDraft}); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if _, err := s.CreateVersion("plan-m", "punakawan", []byte("# v1"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	m, err := s.Manifest("plan-m")
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	if m.CurrentVersion != 1 {
		t.Fatalf("current_version = %d, want 1 after CreateVersion", m.CurrentVersion)
	}
	if m.Status != PlanStatusDraft {
		t.Fatalf("status = %q, want draft (CreateVersion must not touch status)", m.Status)
	}

	// Without a manifest, CreateVersion must not synthesize one.
	if _, err := s.CreateVersion("plan-none", "punakawan", []byte("# v1"), time.Now()); err != nil {
		t.Fatalf("CreateVersion (manifest-less): %v", err)
	}
	if _, err := os.Stat(s.manifestPath("plan-none")); !os.IsNotExist(err) {
		t.Fatalf("CreateVersion synthesized a manifest for a manifest-less plan (stat err = %v)", err)
	}
}
