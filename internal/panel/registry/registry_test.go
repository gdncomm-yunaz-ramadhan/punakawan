package registry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workspaces.yaml")
	s, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	return s
}

func TestOpenAtCreatesEmptyRegistry(t *testing.T) {
	s := openTest(t)
	entries, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("List = %+v, want empty", entries)
	}
}

func TestRegisterAndGet(t *testing.T) {
	s := openTest(t)
	dir := t.TempDir()
	now := time.Now().UTC()

	entry, err := s.Register("checkout-platform", dir, "Checkout Platform", now)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if entry.Id != "checkout-platform" || entry.Path != dir {
		t.Fatalf("Register = %+v", entry)
	}

	got, err := s.Get("checkout-platform")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName == nil || *got.DisplayName != "Checkout Platform" {
		t.Fatalf("Get.DisplayName = %v, want Checkout Platform", got.DisplayName)
	}
}

func TestRegisterRejectsMissingPath(t *testing.T) {
	s := openTest(t)
	if _, err := s.Register("nope", filepath.Join(t.TempDir(), "does-not-exist"), "", time.Now().UTC()); err == nil {
		t.Fatal("expected an error registering a path that does not exist")
	}
}

func TestRegisterRejectsDuplicatePath(t *testing.T) {
	s := openTest(t)
	dir := t.TempDir()
	now := time.Now().UTC()

	if _, err := s.Register("a", dir, "", now); err != nil {
		t.Fatalf("Register(a): %v", err)
	}
	if _, err := s.Register("b", dir, "", now); !errors.Is(err, ErrDuplicatePath) {
		t.Fatalf("Register(b) err = %v, want ErrDuplicatePath", err)
	}
}

func TestRegisterSameIDIsIdempotent(t *testing.T) {
	s := openTest(t)
	dir := t.TempDir()
	first := time.Now().UTC()

	if _, err := s.Register("a", dir, "First", first); err != nil {
		t.Fatalf("Register (first): %v", err)
	}

	second := first.Add(time.Hour)
	entry, err := s.Register("a", dir, "Renamed", second)
	if err != nil {
		t.Fatalf("Register (second): %v", err)
	}
	if entry.DisplayName == nil || *entry.DisplayName != "Renamed" {
		t.Fatalf("DisplayName = %v, want Renamed", entry.DisplayName)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("List = %+v, want exactly one entry (re-registration must not duplicate)", all)
	}
}

func TestRemove(t *testing.T) {
	s := openTest(t)
	dir := t.TempDir()
	if _, err := s.Register("a", dir, "", time.Now().UTC()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := s.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := s.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Remove err = %v, want ErrNotFound", err)
	}
	if err := s.Remove("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Remove (again) err = %v, want ErrNotFound", err)
	}
}

func TestSetPinned(t *testing.T) {
	s := openTest(t)
	dir := t.TempDir()
	if _, err := s.Register("a", dir, "", time.Now().UTC()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := s.SetPinned("a", true); err != nil {
		t.Fatalf("SetPinned: %v", err)
	}
	got, err := s.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Pinned == nil || !*got.Pinned {
		t.Fatalf("Pinned = %v, want true", got.Pinned)
	}
}

func TestSetPinnedUnknownIDErrors(t *testing.T) {
	s := openTest(t)
	if err := s.SetPinned("no-such-id", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDefaultPathHonorsEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom-workspaces.yaml")
	t.Setenv(pathOverrideEnv, override)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if path != override {
		t.Fatalf("DefaultPath = %q, want %q", path, override)
	}
}

func TestDefaultPathIsUnderConfigDir(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	want := filepath.Join(configDir, "punakawan", "workspaces.yaml")
	if path != want {
		t.Fatalf("DefaultPath = %q, want %q", path, want)
	}
}
