package hub

import (
	"os"
	"testing"
)

func TestLookupReturnsNotFoundWhenNoRefFileExists(t *testing.T) {
	root := t.TempDir()
	ref, ok, err := Lookup(root)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false on a project with no hub-ref, got %+v", ref)
	}
}

func TestWriteThenLookupRoundTrips(t *testing.T) {
	root := t.TempDir()
	want := Ref{HubDir: "/var/punakawan/hub", ProjectID: "punokawan"}
	if err := Write(root, want); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, ok, err := Lookup(root)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true after Write")
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestWriteRejectsIncompleteRef(t *testing.T) {
	root := t.TempDir()
	if err := Write(root, Ref{HubDir: "/var/punakawan/hub"}); err == nil {
		t.Fatal("expected an error when project_id is missing")
	}
	if err := Write(root, Ref{ProjectID: "punokawan"}); err == nil {
		t.Fatal("expected an error when hub_dir is missing")
	}
}

func TestLookupRejectsIncompleteRefFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(path(root)[:len(path(root))-len("/"+refFile)], 0o755); err != nil {
		t.Fatal(err)
	}
	// A hand-edited or corrupted ref file (Write itself already guards
	// against ever producing one) must fail loudly, not silently fall back
	// to the legacy path with a half-valid pointer.
	if err := os.WriteFile(path(root), []byte("hub_dir: /var/punakawan/hub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Lookup(root); err == nil {
		t.Fatal("expected an error when the ref file is missing project_id")
	}
}
