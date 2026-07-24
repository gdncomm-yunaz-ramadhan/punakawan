package workflowdef

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func sampleDef(id string) Definition {
	return Definition{
		Version:             SchemaVersion,
		ID:                  id,
		Name:                "Sample " + id,
		Description:         "a sample",
		Enabled:             true,
		RequiredMetadata:    []string{"jira.project_key"},
		Inputs:              []Input{{Name: "include_subtasks", Type: "boolean", Required: false, Default: false}},
		Steps:               []Step{{ID: "s1", Capability: "search_knowledge", Intent: "x"}},
		AllowedCapabilities: []string{"search_knowledge"},
		Approval:            ApprovalPolicy{RequiredFor: []string{"external_write"}},
		Output:              OutputSpec{Type: "jira_issue_list"},
	}
}

func TestStoreSaveGetList(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// List on empty store returns nil, no error.
	defs, err := s.List()
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("want empty list, got %d", len(defs))
	}

	saved, err := s.Save(sampleDef("alpha"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Revision != 1 {
		t.Fatalf("first save revision = %d, want 1", saved.Revision)
	}

	// One file per id, named <id>.yaml.
	if _, err := os.Stat(filepath.Join(root, ".punakawan", "workflows", "alpha.yaml")); err != nil {
		t.Fatalf("expected alpha.yaml: %v", err)
	}

	got, err := s.Get("alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Sample alpha" || got.Revision != 1 || !got.Enabled {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if len(got.Steps) != 1 || got.Steps[0].Capability != "search_knowledge" {
		t.Fatalf("steps not preserved: %+v", got.Steps)
	}

	if _, err := s.Save(sampleDef("beta")); err != nil {
		t.Fatalf("Save beta: %v", err)
	}
	defs, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(defs) != 2 || defs[0].ID != "alpha" || defs[1].ID != "beta" {
		t.Fatalf("List sorted mismatch: %+v", defs)
	}
}

func TestStoreGetNotFound(t *testing.T) {
	s, _ := Open(t.TempDir())
	_, err := s.Get("nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestStoreVersioning(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root)

	first, _ := s.Save(sampleDef("alpha")) // rev 1
	// Edit at the correct base revision -> rev 2, prior snapshotted.
	edit := first
	edit.Description = "changed"
	second, err := s.Save(edit)
	if err != nil {
		t.Fatalf("Save edit: %v", err)
	}
	if second.Revision != 2 {
		t.Fatalf("edit revision = %d, want 2", second.Revision)
	}

	// Prior revision 1 snapshotted under versions/alpha/1.yaml.
	snap := filepath.Join(root, ".punakawan", "workflows", "versions", "alpha", "1.yaml")
	if _, err := os.Stat(snap); err != nil {
		t.Fatalf("expected snapshot %s: %v", snap, err)
	}

	// Current file reflects the edit.
	cur, _ := s.Get("alpha")
	if cur.Description != "changed" || cur.Revision != 2 {
		t.Fatalf("current mismatch: %+v", cur)
	}
}

func TestStoreRevisionConflict(t *testing.T) {
	s, _ := Open(t.TempDir())
	first, _ := s.Save(sampleDef("alpha")) // rev 1
	_, _ = s.Save(mutateRev(first))        // rev 2

	// Re-saving against the stale rev-1 copy conflicts.
	stale := first // Revision == 1
	stale.Description = "late"
	_, err := s.Save(stale)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("want ErrRevisionConflict, got %v", err)
	}
}

func mutateRev(d Definition) Definition {
	d.Description = "v2"
	return d
}

func TestStoreSetEnabled(t *testing.T) {
	root := t.TempDir()
	s, _ := Open(root)
	s.Save(sampleDef("alpha")) // enabled=true, rev 1

	off, err := s.SetEnabled("alpha", false)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if off.Enabled || off.Revision != 2 {
		t.Fatalf("disable mismatch: %+v", off)
	}

	// Snapshot of rev 1 exists.
	snap := filepath.Join(root, ".punakawan", "workflows", "versions", "alpha", "1.yaml")
	if _, err := os.Stat(snap); err != nil {
		t.Fatalf("expected snapshot: %v", err)
	}

	// No-op when already in the target state: revision unchanged.
	same, err := s.SetEnabled("alpha", false)
	if err != nil {
		t.Fatalf("SetEnabled noop: %v", err)
	}
	if same.Revision != 2 {
		t.Fatalf("noop should not bump revision, got %d", same.Revision)
	}

	if _, err := s.SetEnabled("missing", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
