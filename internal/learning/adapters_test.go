package learning

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/project"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

func TestWorkflowAdapterCreateVersionNeverEnables(t *testing.T) {
	root := t.TempDir()
	store, err := workflowdef.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(workflowdef.Definition{Version: workflowdef.SchemaVersion, ID: "wf", Name: "v1", Enabled: false}); err != nil {
		t.Fatal(err)
	}

	adapter := &WorkflowAdapter{Root: root}
	ref, err := adapter.Current("wf")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Version != 1 {
		t.Fatalf("current version = %d, want 1", ref.Version)
	}

	// Candidate tries to enable the workflow; acceptance must NOT honor that.
	candidate, _ := json.Marshal(workflowdef.Definition{Version: workflowdef.SchemaVersion, ID: "wf", Name: "v2", Enabled: true})
	newRef, err := adapter.CreateVersion("wf", "ws", candidate, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if newRef.Version != 2 {
		t.Fatalf("new version = %d, want 2", newRef.Version)
	}
	got, _ := store.Get("wf")
	if got.Name != "v2" {
		t.Fatalf("name not applied: %q", got.Name)
	}
	if got.Enabled {
		t.Fatal("acceptance must never enable a workflow (activation is separate)")
	}
}

func TestMetadataAdapterCreateAndUpdate(t *testing.T) {
	root := t.TempDir()
	adapter := &MetadataAdapter{Root: root}

	// Create a new metadata entry via acceptance.
	create, _ := json.Marshal(project.MetadataEntry{Key: "test.command", Description: "how to test", Value: "go test ./..."})
	ref1, err := adapter.CreateVersion("test.command", "ws", create, time.Now())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	proj, _ := project.Load(root)
	if entry, ok := proj.MetadataFor("test.command"); !ok || entry.Value != "go test ./..." {
		t.Fatalf("metadata not written: %+v", entry)
	}

	// A stale base is detectable: the revision hash moves after a project change.
	other, _ := json.Marshal(project.MetadataEntry{Key: "release.owner", Description: "who owns releases", Value: "team-x"})
	if _, err := adapter.CreateVersion("release.owner", "ws", other, time.Now()); err != nil {
		t.Fatal(err)
	}
	ref3, err := adapter.Current("test.command")
	if err != nil {
		t.Fatal(err)
	}
	if ref3.RevisionHash == ref1.RevisionHash {
		t.Fatal("revision hash should change after an intervening project mutation (stale-base detection)")
	}

	// Update the existing entry.
	upd, _ := json.Marshal(project.MetadataEntry{Key: "test.command", Description: "how to test", Value: "make test"})
	if _, err := adapter.CreateVersion("test.command", "ws", upd, time.Now()); err != nil {
		t.Fatalf("update: %v", err)
	}
	proj, _ = project.Load(root)
	if entry, _ := proj.MetadataFor("test.command"); entry.Value != "make test" {
		t.Fatalf("update not applied: %+v", entry)
	}
}
