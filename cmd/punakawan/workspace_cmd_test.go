package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// setTestRegistry redirects the panel workspace registry to a throwaway
// file for the duration of the test, so these tests never touch the
// developer's real ~/.config/punakawan (or platform equivalent).
func setTestRegistry(t *testing.T) {
	t.Helper()
	t.Setenv("PUNAKAWAN_PANEL_REGISTRY_PATH", filepath.Join(t.TempDir(), "workspaces.yaml"))
}

func TestWorkspaceRegisterAndList(t *testing.T) {
	setTestRegistry(t)
	dir := newSmokeWorkspace(t)

	out, err := runCLI(t, dir, "workspace", "register", ".")
	if err != nil {
		t.Fatalf("workspace register: %v\n%s", err, out)
	}
	if !strings.Contains(out, "smoke") {
		t.Fatalf("register output = %q, want it to mention the workspace id", out)
	}

	out, err = runCLI(t, dir, "workspace", "list")
	if err != nil {
		t.Fatalf("workspace list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "smoke") {
		t.Fatalf("list output = %q, want it to mention smoke", out)
	}
}

func TestWorkspaceListEmptyRegistry(t *testing.T) {
	setTestRegistry(t)
	dir := newSmokeWorkspace(t)

	out, err := runCLI(t, dir, "workspace", "list")
	if err != nil {
		t.Fatalf("workspace list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no workspaces are registered") {
		t.Fatalf("list output = %q, want the empty-registry message", out)
	}
}

func TestWorkspacePinUnpinAndRemove(t *testing.T) {
	setTestRegistry(t)
	dir := newSmokeWorkspace(t)

	if _, err := runCLI(t, dir, "workspace", "register", "."); err != nil {
		t.Fatalf("workspace register: %v", err)
	}

	if _, err := runCLI(t, dir, "workspace", "pin", "smoke"); err != nil {
		t.Fatalf("workspace pin: %v", err)
	}
	out, err := runCLI(t, dir, "workspace", "list")
	if err != nil {
		t.Fatalf("workspace list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "pinned") {
		t.Fatalf("list output = %q, want it to show pinned", out)
	}

	if _, err := runCLI(t, dir, "workspace", "unpin", "smoke"); err != nil {
		t.Fatalf("workspace unpin: %v", err)
	}
	out, err = runCLI(t, dir, "workspace", "list")
	if err != nil {
		t.Fatalf("workspace list: %v\n%s", err, out)
	}
	if strings.Contains(out, "pinned") {
		t.Fatalf("list output = %q, want no pinned marker after unpin", out)
	}

	if _, err := runCLI(t, dir, "workspace", "remove", "smoke"); err != nil {
		t.Fatalf("workspace remove: %v", err)
	}
	out, err = runCLI(t, dir, "workspace", "list")
	if err != nil {
		t.Fatalf("workspace list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "no workspaces are registered") {
		t.Fatalf("list output after remove = %q, want empty-registry message", out)
	}
}

func TestWorkspaceRemoveUnknownIDErrors(t *testing.T) {
	setTestRegistry(t)
	dir := newSmokeWorkspace(t)

	if _, err := runCLI(t, dir, "workspace", "remove", "no-such-id"); err == nil {
		t.Fatal("expected an error removing an unregistered workspace id")
	}
}
