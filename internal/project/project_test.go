package project

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeWorkspace writes a minimal valid workspace.yaml so Load's synthesize
// path can resolve a name/id through internal/workspace.
func writeWorkspace(t *testing.T, root, id, name string) {
	t.Helper()
	dir := filepath.Join(root, ".punakawan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "version: punakawan.workspace/v1\nid: " + id + "\nname: " + name +
		"\nrepositories:\n  - id: " + id + "\n    path: .\n"
	if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAbsentSynthesizesFromWorkspace(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, "affiliate-platform", "Affiliate Platform")

	p, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.ID != "affiliate-platform" {
		t.Errorf("ID = %q, want affiliate-platform", p.ID)
	}
	if p.Name != "Affiliate Platform" {
		t.Errorf("Name = %q, want Affiliate Platform", p.Name)
	}
	if p.Revision != 0 {
		t.Errorf("Revision = %d, want 0", p.Revision)
	}
	if len(p.Metadata) != 0 {
		t.Errorf("Metadata = %+v, want empty", p.Metadata)
	}
	if p.Path != root {
		t.Errorf("Path = %q, want %q", p.Path, root)
	}
}

func TestLoadAbsentNoWorkspaceUsesBaseName(t *testing.T) {
	root := t.TempDir()
	// No workspace.yaml and no .git: Load must still not error.
	p, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.ID != filepath.Base(root) {
		t.Errorf("ID = %q, want %q", p.ID, filepath.Base(root))
	}
	if p.Revision != 0 {
		t.Errorf("Revision = %d, want 0", p.Revision)
	}
}

func TestSaveLoadRoundTripAndVersioning(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, "proj", "Proj")

	p, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)

	// Revision 0 -> 1: add an entry.
	if err := p.AddMetadata(MetadataEntry{Key: "jira.project_key", Description: "Jira key", Value: "TRF"}, 0); err != nil {
		t.Fatalf("AddMetadata: %v", err)
	}
	if err := Save(root, p, SaveOptions{Now: now, Action: "add", Key: "jira.project_key"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload and mutate again: 1 -> 2.
	p2, err := Load(root)
	if err != nil {
		t.Fatalf("Load 2: %v", err)
	}
	if p2.Revision != 1 {
		t.Fatalf("reloaded Revision = %d, want 1", p2.Revision)
	}
	if got, ok := p2.MetadataFor("jira.project_key"); !ok || got.Value != "TRF" {
		t.Fatalf("MetadataFor = %+v ok=%v, want value TRF", got, ok)
	}
	if err := p2.AddMetadata(MetadataEntry{Key: "team.owner", Description: "Owner team", Value: "AFF"}, 1); err != nil {
		t.Fatalf("AddMetadata 2: %v", err)
	}
	if err := Save(root, p2, SaveOptions{Now: now, Action: "add", Key: "team.owner"}); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	// Immutable version history: the first Save (0->1) had no pre-mutation
	// file on disk (rev 0 was synthesized in memory, never persisted), so
	// only rev 1's file is snapshotted when the second Save (1->2) overwrites
	// it. There is intentionally no 0.yaml.
	versionsDir := filepath.Join(root, ".punakawan", "project", "versions")
	if _, err := os.Stat(filepath.Join(versionsDir, "1.yaml")); err != nil {
		t.Errorf("expected version snapshot 1.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(versionsDir, "0.yaml")); !os.IsNotExist(err) {
		t.Errorf("did not expect 0.yaml (rev 0 was never persisted): err=%v", err)
	}

	// Audit log: two lines, first old=0/new=1, second old=1/new=2.
	auditPath := filepath.Join(root, ".punakawan", "project", "audit.jsonl")
	f, err := os.Open(auditPath)
	if err != nil {
		t.Fatalf("open audit: %v", err)
	}
	defer f.Close()
	var recs []auditRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec auditRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("parse audit: %v", err)
		}
		recs = append(recs, rec)
	}
	if len(recs) != 2 {
		t.Fatalf("audit records = %d, want 2", len(recs))
	}
	if recs[0].OldRevision != 0 || recs[0].NewRevision != 1 || recs[0].Actor != DefaultActor || recs[0].Key != "jira.project_key" {
		t.Errorf("audit[0] = %+v, unexpected", recs[0])
	}
	if recs[1].OldRevision != 1 || recs[1].NewRevision != 2 || recs[1].Action != "add" {
		t.Errorf("audit[1] = %+v, unexpected", recs[1])
	}
	if !recs[0].Ts.Equal(now) {
		t.Errorf("audit[0].Ts = %v, want %v", recs[0].Ts, now)
	}
}

func TestVersionSnapshotIsImmutable(t *testing.T) {
	root := t.TempDir()
	writeWorkspace(t, root, "proj", "Proj")

	// Save 0->1 (no snapshot: rev 0 never on disk), 1->2 (snapshots rev 1).
	p, _ := Load(root)
	_ = p.AddMetadata(MetadataEntry{Key: "a.b", Description: "d", Value: "v"}, 0)
	if err := Save(root, p, SaveOptions{Action: "add", Key: "a.b"}); err != nil {
		t.Fatal(err)
	}
	p2, _ := Load(root)
	_ = p2.AddMetadata(MetadataEntry{Key: "c.d", Description: "d", Value: "v"}, 1)
	if err := Save(root, p2, SaveOptions{Action: "add", Key: "c.d"}); err != nil {
		t.Fatal(err)
	}

	snap := filepath.Join(root, ".punakawan", "project", "versions", "1.yaml")
	before, err := os.ReadFile(snap)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	// A third save (2->3) snapshots rev 2, and must leave the rev-1 snapshot
	// untouched: accepted versions are immutable.
	p3, _ := Load(root)
	_ = p3.AddMetadata(MetadataEntry{Key: "e.f", Description: "d", Value: "v"}, 2)
	if err := Save(root, p3, SaveOptions{Action: "add", Key: "e.f"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(snap)
	if err != nil {
		t.Fatalf("read snapshot after: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("rev-1 snapshot changed after a later save; must be immutable")
	}
}

func TestOptimisticLocking(t *testing.T) {
	p := &Project{ID: "p", Revision: 3, Metadata: []MetadataEntry{}}
	err := p.AddMetadata(MetadataEntry{Key: "x.y", Description: "d", Value: "v"}, 2)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("AddMetadata stale base: err = %v, want ErrRevisionConflict", err)
	}
	if p.Revision != 3 || len(p.Metadata) != 0 {
		t.Fatalf("conflict must not mutate: rev=%d len=%d", p.Revision, len(p.Metadata))
	}
	// Correct base succeeds and bumps.
	if err := p.AddMetadata(MetadataEntry{Key: "x.y", Description: "d", Value: "v"}, 3); err != nil {
		t.Fatalf("AddMetadata: %v", err)
	}
	if p.Revision != 4 {
		t.Fatalf("Revision = %d, want 4", p.Revision)
	}
}

func TestUpdateAndDeleteMetadata(t *testing.T) {
	p := &Project{ID: "p", Revision: 0, Metadata: []MetadataEntry{}}
	if err := p.AddMetadata(MetadataEntry{Key: "build.command", Description: "build", Value: "make"}, 0); err != nil {
		t.Fatal(err)
	}
	// Update value only, description untouched.
	newVal := "make build"
	if err := p.UpdateMetadata("build.command", nil, newVal, 1); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	got, _ := p.MetadataFor("build.command")
	if got.Value != "make build" || got.Description != "build" {
		t.Fatalf("after update = %+v, want value 'make build' desc 'build'", got)
	}
	if p.Revision != 2 {
		t.Fatalf("Revision = %d, want 2", p.Revision)
	}
	// Update unknown key.
	if err := p.UpdateMetadata("no.such", nil, "x", 2); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("update unknown: err = %v, want ErrKeyNotFound", err)
	}
	// Delete with wrong base.
	if err := p.DeleteMetadata("build.command", 99); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("delete stale: err = %v, want ErrRevisionConflict", err)
	}
	// Delete correctly.
	if err := p.DeleteMetadata("build.command", 2); err != nil {
		t.Fatalf("DeleteMetadata: %v", err)
	}
	if _, ok := p.MetadataFor("build.command"); ok {
		t.Fatalf("entry still present after delete")
	}
	if p.Revision != 3 {
		t.Fatalf("Revision = %d, want 3", p.Revision)
	}
}

func TestCaseInsensitiveDuplicateKey(t *testing.T) {
	p := &Project{ID: "p", Revision: 0, Metadata: []MetadataEntry{}}
	if err := p.AddMetadata(MetadataEntry{Key: "Jira.Key", Description: "d", Value: "v"}, 0); err != nil {
		t.Fatal(err)
	}
	err := p.AddMetadata(MetadataEntry{Key: "jira.key", Description: "d2", Value: "v2"}, 1)
	if !errors.Is(err, ErrDuplicateKey) {
		t.Fatalf("duplicate (case-insensitive): err = %v, want ErrDuplicateKey", err)
	}
}
