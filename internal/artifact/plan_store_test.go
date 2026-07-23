package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestPlanStoreCreateVersionAssignsSequentialNumbers(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}
	now := time.Now()

	v1, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v1"), now)
	if err != nil {
		t.Fatalf("CreateVersion(v1): %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("v1.Version = %d, want 1", v1.Version)
	}

	v2, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v2"), now)
	if err != nil {
		t.Fatalf("CreateVersion(v2): %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("v2.Version = %d, want 2", v2.Version)
	}
	if v2.RevisionHash == v1.RevisionHash {
		t.Fatal("v1 and v2 have the same revision hash despite different content")
	}
}

func TestPlanStoreCurrentTracksLatest(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}
	now := time.Now()

	if _, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v1"), now); err != nil {
		t.Fatalf("CreateVersion(v1): %v", err)
	}
	v2, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v2"), now)
	if err != nil {
		t.Fatalf("CreateVersion(v2): %v", err)
	}

	current, err := s.Current("plan-panel")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.Version != 2 || current.RevisionHash != v2.RevisionHash {
		t.Fatalf("Current = %+v, want version 2 matching v2's hash", current)
	}
}

func TestPlanStoreVersionReadsAnyImmutableVersion(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}
	now := time.Now()

	if _, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v1"), now); err != nil {
		t.Fatalf("CreateVersion(v1): %v", err)
	}
	if _, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v2"), now); err != nil {
		t.Fatalf("CreateVersion(v2): %v", err)
	}

	content, ref, err := s.Version("plan-panel", 1)
	if err != nil {
		t.Fatalf("Version(1): %v", err)
	}
	if string(content) != "# v1" {
		t.Fatalf("content = %q, want %q", content, "# v1")
	}
	if ref.Version != 1 {
		t.Fatalf("ref.Version = %d, want 1", ref.Version)
	}
	if ref.RevisionHash != Hash([]byte("# v1")) {
		t.Fatalf("ref.RevisionHash = %q, want the hash of v1's content", ref.RevisionHash)
	}
}

func TestPlanStoreVersionNotFound(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}
	if _, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v1"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	if _, _, err := s.Version("plan-panel", 99); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("err = %v, want ErrVersionNotFound", err)
	}
}

func TestPlanStoreCurrentNotFoundForUnknownPlan(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}
	if _, err := s.Current("no-such-plan"); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("err = %v, want ErrPlanNotFound", err)
	}
}

func TestPlanStoreSkipsPastAStrayVersionFileWithoutTouchingIt(t *testing.T) {
	root := t.TempDir()
	s := &PlanStore{WorkspaceRoot: root}
	if _, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v1"), time.Now()); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	// A stray file at the slot CreateVersion would otherwise pick next
	// (e.g. left over from external tampering) must never be
	// overwritten - latestVersion() sees it occupied and CreateVersion
	// advances past it instead.
	stray := filepath.Join(root, ".punakawan", "plans", "plan-panel", "versions", "2.md")
	if err := os.WriteFile(stray, []byte("already here"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	next, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v3"), time.Now())
	if err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	if next.Version != 3 {
		t.Fatalf("next.Version = %d, want 3 (skipping past the occupied slot 2)", next.Version)
	}

	data, err := os.ReadFile(stray)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "already here" {
		t.Fatalf("stray file content = %q, want it untouched", data)
	}
}

func TestPlanStoreReferenceMatchesArtifactReferenceSchema(t *testing.T) {
	s := &PlanStore{WorkspaceRoot: t.TempDir()}
	ref, err := s.CreateVersion("plan-panel", "punakawan", []byte("# v1"), time.Now())
	if err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}
	if ref.Type != protocol.ArtifactReferenceTypePlan {
		t.Fatalf("Type = %q, want plan", ref.Type)
	}
	if ref.Format != protocol.ArtifactReferenceFormatMarkdown {
		t.Fatalf("Format = %q, want markdown", ref.Format)
	}
	if ref.CanonicalLocation == nil || *ref.CanonicalLocation != filepath.Join(".punakawan", "plans", "plan-panel", "versions", "1.md") {
		t.Fatalf("CanonicalLocation = %v, want the workspace-relative versions/1.md path", ref.CanonicalLocation)
	}
}
