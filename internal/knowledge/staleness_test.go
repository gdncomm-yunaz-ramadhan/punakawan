package knowledge

import (
	"regexp"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

var contentHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestContentHashMatchesSchemaPattern(t *testing.T) {
	got := ContentHash([]byte("hello world"))
	if !contentHashPattern.MatchString(got) {
		t.Fatalf("ContentHash(%q) = %q, does not match schema pattern %s", "hello world", got, contentHashPattern)
	}

	// Known SHA-256 digest of "hello world" (verified via `shasum -a 256`).
	want := "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if got != want {
		t.Fatalf("ContentHash(%q) = %q, want %q", "hello world", got, want)
	}
}

func TestContentHashIsDeterministic(t *testing.T) {
	a := ContentHash([]byte("same input"))
	b := ContentHash([]byte("same input"))
	if a != b {
		t.Fatalf("ContentHash not deterministic: %q != %q", a, b)
	}

	c := ContentHash([]byte("different input"))
	if a == c {
		t.Fatalf("ContentHash collided for different inputs: %q", a)
	}
}

func TestCheckStaleMatchingHashLeavesValidityUnchanged(t *testing.T) {
	store := newTestStore(t)

	hash := ContentHash([]byte("original content"))
	rec := validRecord()
	rec.Source.ContentHash = &hash
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	stale, err := store.CheckStale(rec.Id, hash)
	if err != nil {
		t.Fatalf("CheckStale: %v", err)
	}
	if stale {
		t.Fatal("expected stale=false when hashes match")
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Validity.State != rec.Validity.State {
		t.Fatalf("expected validity.state unchanged (%q), got %q", rec.Validity.State, got.Validity.State)
	}
}

func TestCheckStaleMismatchedHashTransitionsToStale(t *testing.T) {
	store := newTestStore(t)

	originalHash := ContentHash([]byte("original content"))
	rec := validRecord()
	rec.Source.ContentHash = &originalHash
	rec.Validity.State = protocol.KnowledgeRecordValidityStateVerified
	rec.Validity.VerifiedBy = []string{"gareng"}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	newHash := ContentHash([]byte("changed content"))
	stale, err := store.CheckStale(rec.Id, newHash)
	if err != nil {
		t.Fatalf("CheckStale: %v", err)
	}
	if !stale {
		t.Fatal("expected stale=true when hashes differ")
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Validity.State != protocol.KnowledgeRecordValidityStateStale {
		t.Fatalf("expected validity.state=stale after CheckStale, got %q", got.Validity.State)
	}
}

func TestCheckStaleNoStoredHashReturnsFalse(t *testing.T) {
	store := newTestStore(t)

	rec := validRecord()
	rec.Source.ContentHash = nil
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	stale, err := store.CheckStale(rec.Id, ContentHash([]byte("anything")))
	if err != nil {
		t.Fatalf("CheckStale: %v", err)
	}
	if stale {
		t.Fatal("expected stale=false when record has no stored content_hash")
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Validity.State != rec.Validity.State {
		t.Fatalf("expected validity.state unchanged (%q), got %q", rec.Validity.State, got.Validity.State)
	}
}
