package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/hub"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newLegacyProjectRoot builds a throwaway project root with a real, populated
// legacy Dolt knowledge store at root/.punakawan/knowledge, matching the
// exact layout MigrateToHub expects (and Open's own dbName convention -
// filepath.Base of the data dir - requires the directory be named
// "knowledge").
func newLegacyProjectRoot(t *testing.T) (root string, sup *tools.Supervisor) {
	t.Helper()
	root = t.TempDir()
	sup = tools.New(root)

	legacyDir := filepath.Join(root, ".punakawan", "knowledge")
	store, err := Open(sup, legacyDir)
	if err != nil {
		t.Fatalf("Open legacy store: %v", err)
	}
	rec := protocol.KnowledgeRecord{
		Id: "pkw:req/fixture/LEGACY-1", Type: protocol.KnowledgeRecordTypeRequirement, Status: "active", Title: "Legacy record",
		Source:     protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{Method: protocol.KnowledgeRecordExtractionMethodManual},
		Validity:   protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put on legacy store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close legacy store: %v", err)
	}
	return root, sup
}

func TestMigrateToHubMovesDataAndRecordsHubRef(t *testing.T) {
	requireDoltForHubTest(t)

	root, _ := newLegacyProjectRoot(t)
	hubDir := filepath.Join(t.TempDir(), "hub")

	if err := MigrateToHub(root, hubDir, "proj-a"); err != nil {
		t.Fatalf("MigrateToHub: %v", err)
	}

	ref, ok, err := hub.Lookup(root)
	if err != nil || !ok {
		t.Fatalf("expected a hub ref after migration, ok=%v err=%v", ok, err)
	}
	if ref.HubDir != hubDir || ref.ProjectID != "proj-a" {
		t.Fatalf("unexpected ref: %+v", ref)
	}

	// The migrated directory must be a genuinely fresh-boot discovery: no hub
	// server has ever run yet, so OpenInHub starting one from scratch must see
	// the copied-in data (verified empirically - a directory placed before a
	// hub server's first boot is scanned in; one added after would not be).
	sup := tools.New(filepath.Dir(hubDir))
	hubStore, err := OpenInHub(sup, hubDir, "proj-a")
	if err != nil {
		t.Fatalf("OpenInHub after migration: %v", err)
	}
	t.Cleanup(func() { _ = hubStore.Close() })

	if _, err := hubStore.Get("pkw:req/fixture/LEGACY-1"); err != nil {
		t.Fatalf("expected migrated record to be readable via the hub: %v", err)
	}

	// Migration copies; it must never delete the legacy directory.
	if _, err := os.Stat(filepath.Join(root, ".punakawan", "knowledge", ".dolt")); err != nil {
		t.Fatalf("expected legacy directory to remain in place after migration: %v", err)
	}
}

func TestMigrateToHubMovesEventsFile(t *testing.T) {
	requireDoltForHubTest(t)

	root, _ := newLegacyProjectRoot(t)
	eventsDir := filepath.Join(root, ".punakawan", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"event":"fixture"}` + "\n")
	if err := os.WriteFile(filepath.Join(eventsDir, "knowledge-events.jsonl"), want, 0o644); err != nil {
		t.Fatal(err)
	}

	hubDir := filepath.Join(t.TempDir(), "hub")
	if err := MigrateToHub(root, hubDir, "proj-events"); err != nil {
		t.Fatalf("MigrateToHub: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(hubDir, "proj-events.events", "knowledge-events.jsonl"))
	if err != nil {
		t.Fatalf("read migrated events file: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("migrated events content mismatch: got %q want %q", got, want)
	}
}

func TestMigrateToHubFailsWhenNoLegacyStoreExists(t *testing.T) {
	root := t.TempDir()
	hubDir := filepath.Join(t.TempDir(), "hub")

	err := MigrateToHub(root, hubDir, "proj-a")
	if !errors.Is(err, ErrNothingToMigrate) {
		t.Fatalf("expected ErrNothingToMigrate, got %v", err)
	}
}

func TestMigrateToHubFailsWhenAlreadyMigrated(t *testing.T) {
	root := t.TempDir()
	hubDir := filepath.Join(t.TempDir(), "hub")
	legacyDir := filepath.Join(root, ".punakawan", "knowledge")
	if err := os.MkdirAll(filepath.Join(legacyDir, ".dolt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := hub.Write(root, hub.Ref{HubDir: hubDir, ProjectID: "already-there"}); err != nil {
		t.Fatal(err)
	}

	err := MigrateToHub(root, hubDir, "proj-a")
	if !errors.Is(err, ErrAlreadyMigrated) {
		t.Fatalf("expected ErrAlreadyMigrated, got %v", err)
	}
}

func TestMigrateToHubRefusesWhenLegacyServerIsRunning(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	sup := tools.New(root)
	legacyDir := filepath.Join(root, ".punakawan", "knowledge")
	store, err := Open(sup, legacyDir)
	if err != nil {
		t.Fatalf("Open legacy store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	hubDir := filepath.Join(t.TempDir(), "hub")
	err = MigrateToHub(root, hubDir, "proj-a")
	if !errors.Is(err, ErrLegacyServerRunning) {
		t.Fatalf("expected ErrLegacyServerRunning while the legacy store is open, got %v", err)
	}
}

func TestMigrateToHubRefusesWhenHubServerIsAlreadyRunning(t *testing.T) {
	requireDoltForHubTest(t)

	root, _ := newLegacyProjectRoot(t)
	hubRoot := t.TempDir()
	hubDir := filepath.Join(hubRoot, "hub")
	sup := tools.New(hubRoot)

	// Start the hub server for an unrelated project first.
	other, err := OpenInHub(sup, hubDir, "other-project")
	if err != nil {
		t.Fatalf("OpenInHub other-project: %v", err)
	}
	t.Cleanup(func() { _ = other.Close() })

	err = MigrateToHub(root, hubDir, "proj-a")
	if !errors.Is(err, ErrHubServerRunning) {
		t.Fatalf("expected ErrHubServerRunning while the hub server is already up, got %v", err)
	}
}

func TestMigrateToHubRefusesWhenTargetProjectDirAlreadyExists(t *testing.T) {
	requireDoltForHubTest(t)

	root, _ := newLegacyProjectRoot(t)
	hubDir := filepath.Join(t.TempDir(), "hub")
	if err := os.MkdirAll(filepath.Join(hubDir, "proj-a"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := MigrateToHub(root, hubDir, "proj-a")
	if !errors.Is(err, ErrHubProjectExists) {
		t.Fatalf("expected ErrHubProjectExists, got %v", err)
	}
}

func TestMigrateToHubRejectsUnsafeProjectID(t *testing.T) {
	root, _ := newLegacyProjectRoot(t)
	hubDir := filepath.Join(t.TempDir(), "hub")

	if err := MigrateToHub(root, hubDir, "proj a"); err == nil {
		t.Fatal("expected an error for an unsafe projectID")
	}
}
