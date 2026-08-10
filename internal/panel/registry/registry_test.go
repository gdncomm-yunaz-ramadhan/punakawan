package registry

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// openTest returns a Store backed by a throwaway storage kernel on a temp
// database, so these tests never touch this machine's real data dir.
func openTest(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

func TestNewStoreIsEmpty(t *testing.T) {
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
	if entry.LastSeenAt == nil {
		t.Fatalf("Register entry.LastSeenAt = nil, want set")
	}

	got, err := s.Get("checkout-platform")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName == nil || *got.DisplayName != "Checkout Platform" {
		t.Fatalf("Get.DisplayName = %v, want Checkout Platform", got.DisplayName)
	}
	if got.Path != dir {
		t.Fatalf("Get.Path = %q, want %q", got.Path, dir)
	}
}

func TestGetUnknownIDErrors(t *testing.T) {
	s := openTest(t)
	if _, err := s.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get err = %v, want ErrNotFound", err)
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

func TestRegisterSameIDKeepsDisplayNameWhenBlank(t *testing.T) {
	s := openTest(t)
	dir := t.TempDir()
	now := time.Now().UTC()

	if _, err := s.Register("a", dir, "Original", now); err != nil {
		t.Fatalf("Register (first): %v", err)
	}
	// A re-registration with an empty display name must not blank the
	// existing one, matching §7's "renaming a display label does not change
	// the stable workspace ID" - and, symmetrically, a non-rename leaves it.
	entry, err := s.Register("a", dir, "", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Register (second): %v", err)
	}
	if entry.DisplayName == nil || *entry.DisplayName != "Original" {
		t.Fatalf("DisplayName = %v, want Original preserved", entry.DisplayName)
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

	// Unpinning the same id must take effect - a stable per-id idempotency
	// key would wrongly collapse this second write.
	if err := s.SetPinned("a", false); err != nil {
		t.Fatalf("SetPinned(false): %v", err)
	}
	got, err = s.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Pinned == nil || *got.Pinned {
		t.Fatalf("Pinned = %v, want false after unpin", got.Pinned)
	}
}

func TestSetPinnedUnknownIDErrors(t *testing.T) {
	s := openTest(t)
	if err := s.SetPinned("no-such-id", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
