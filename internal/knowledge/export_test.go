package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/hub"
	"github.com/ygrip/punakawan/internal/tools"
)

func TestExportFromHubProducesAGitPortableSnapshotWithFullHistory(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	hubDir := filepath.Join(root, "hub")
	sup := tools.New(root)

	if err := hub.Write(root, hub.Ref{HubDir: hubDir, ProjectID: "proj-export"}); err != nil {
		t.Fatalf("hub.Write: %v", err)
	}

	store, err := OpenInHub(sup, hubDir, "proj-export")
	if err != nil {
		t.Fatalf("OpenInHub: %v", err)
	}
	rec := newHubRecord("pkw:req/fixture/EXPORT-1", "exported record")
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// The hub server stays live across the export - this is the whole point
	// of using CALL DOLT_BACKUP instead of copying files out from under it.
	t.Cleanup(func() { _ = store.Close() })

	if err := ExportFromHub(sup, root); err != nil {
		t.Fatalf("ExportFromHub: %v", err)
	}

	snapshotDir := filepath.Join(root, ".punakawan", "knowledge")
	if _, err := os.Stat(filepath.Join(snapshotDir, ".dolt")); err != nil {
		t.Fatalf("expected a standalone Dolt repo at %s: %v", snapshotDir, err)
	}

	// Open the snapshot exactly as the legacy path would, to prove it is a
	// genuinely standalone, functional repository, not an inert byte copy.
	snapshotStore, err := Open(sup, snapshotDir)
	if err != nil {
		t.Fatalf("Open snapshot: %v", err)
	}
	t.Cleanup(func() { _ = snapshotStore.Close() })
	got, err := snapshotStore.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get from snapshot: %v", err)
	}
	if got.Title != rec.Title {
		t.Fatalf("got title %q, want %q", got.Title, rec.Title)
	}
}

func TestExportFromHubIsRepeatable(t *testing.T) {
	requireDoltForHubTest(t)

	root := t.TempDir()
	hubDir := filepath.Join(root, "hub")
	sup := tools.New(root)
	if err := hub.Write(root, hub.Ref{HubDir: hubDir, ProjectID: "proj-repeat"}); err != nil {
		t.Fatalf("hub.Write: %v", err)
	}
	store, err := OpenInHub(sup, hubDir, "proj-repeat")
	if err != nil {
		t.Fatalf("OpenInHub: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.Put(newHubRecord("pkw:req/fixture/REPEAT-1", "first")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := ExportFromHub(sup, root); err != nil {
		t.Fatalf("first ExportFromHub: %v", err)
	}

	if err := store.Put(newHubRecord("pkw:req/fixture/REPEAT-2", "second")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := ExportFromHub(sup, root); err != nil {
		t.Fatalf("second ExportFromHub: %v", err)
	}

	snapshotDir := filepath.Join(root, ".punakawan", "knowledge")
	snapshotStore, err := Open(sup, snapshotDir)
	if err != nil {
		t.Fatalf("Open snapshot: %v", err)
	}
	t.Cleanup(func() { _ = snapshotStore.Close() })

	if _, err := snapshotStore.Get("pkw:req/fixture/REPEAT-1"); err != nil {
		t.Fatalf("expected REPEAT-1 in the refreshed snapshot: %v", err)
	}
	if _, err := snapshotStore.Get("pkw:req/fixture/REPEAT-2"); err != nil {
		t.Fatalf("expected REPEAT-2 in the refreshed snapshot: %v", err)
	}
}

func TestExportFromHubFailsWhenNotHubBacked(t *testing.T) {
	root := t.TempDir()
	sup := tools.New(root)

	err := ExportFromHub(sup, root)
	if !errors.Is(err, ErrNotHubBacked) {
		t.Fatalf("expected ErrNotHubBacked, got %v", err)
	}
}

func TestImportToHubSeedsFromAnExportedSnapshot(t *testing.T) {
	requireDoltForHubTest(t)

	// Simulate machine A: export a snapshot.
	rootA := t.TempDir()
	hubDirA := filepath.Join(rootA, "hub")
	supA := tools.New(rootA)
	if err := hub.Write(rootA, hub.Ref{HubDir: hubDirA, ProjectID: "proj-portable"}); err != nil {
		t.Fatalf("hub.Write: %v", err)
	}
	storeA, err := OpenInHub(supA, hubDirA, "proj-portable")
	if err != nil {
		t.Fatalf("OpenInHub: %v", err)
	}
	rec := newHubRecord("pkw:req/fixture/PORTABLE-1", "portable record")
	if err := storeA.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := ExportFromHub(supA, rootA); err != nil {
		t.Fatalf("ExportFromHub: %v", err)
	}
	if err := storeA.Close(); err != nil {
		t.Fatalf("close storeA: %v", err)
	}

	// Simulate machine B: only the git-tracked .punakawan/knowledge snapshot
	// survives the "clone the repo onto a fresh machine" step - build a fresh
	// root with just that directory copied over, no hub-ref, no live server.
	rootB := t.TempDir()
	if err := os.CopyFS(filepath.Join(rootB, ".punakawan", "knowledge"), os.DirFS(filepath.Join(rootA, ".punakawan", "knowledge"))); err != nil {
		t.Fatalf("simulate cloned snapshot: %v", err)
	}
	hubDirB := filepath.Join(rootB, "hub")

	if err := ImportToHub(rootB, hubDirB, "proj-portable"); err != nil {
		t.Fatalf("ImportToHub: %v", err)
	}

	ref, ok, err := hub.Lookup(rootB)
	if err != nil || !ok {
		t.Fatalf("expected a hub ref after import, ok=%v err=%v", ok, err)
	}
	if ref.HubDir != hubDirB || ref.ProjectID != "proj-portable" {
		t.Fatalf("unexpected ref: %+v", ref)
	}

	supB := tools.New(filepath.Dir(hubDirB))
	hubStoreB, err := OpenInHub(supB, hubDirB, "proj-portable")
	if err != nil {
		t.Fatalf("OpenInHub after import: %v", err)
	}
	t.Cleanup(func() { _ = hubStoreB.Close() })
	got, err := hubStoreB.Get(rec.Id)
	if err != nil {
		t.Fatalf("expected the imported record to be readable via the hub: %v", err)
	}
	if got.Title != rec.Title {
		t.Fatalf("got title %q, want %q", got.Title, rec.Title)
	}
}
