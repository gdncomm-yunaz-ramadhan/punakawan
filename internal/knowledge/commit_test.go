package knowledge

import (
	"regexp"
	"testing"
)

// TestCommitWorkingSetReturnsAStableAuditedHash verifies the SQLite-era
// CommitWorkingSet contract: a non-empty content-hash identifier is returned
// and the message is recorded once in the shared kernel's audit_log under that
// hash.
func TestCommitWorkingSetReturnsAStableAuditedHash(t *testing.T) {
	store := newTestStore(t)

	hash, err := store.CommitWorkingSet("test: delete pkw:req/fixture/COMMIT-1")
	if err != nil {
		t.Fatalf("CommitWorkingSet: %v", err)
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(hash) {
		t.Fatalf("hash = %q, want a sha256:<hex> identifier", hash)
	}

	// The message was recorded in the audit trail keyed by the returned hash.
	var summary string
	if err := store.db.Reader().QueryRow(`SELECT summary FROM audit_log WHERE idempotency_key = ?`, hash).Scan(&summary); err != nil {
		t.Fatalf("query audit_log for the commit: %v", err)
	}
	if summary != "test: delete pkw:req/fixture/COMMIT-1" {
		t.Fatalf("audit summary = %q, want the message passed to CommitWorkingSet", summary)
	}
}

// TestCommitWorkingSetDistinctMessagesDistinctHashes shows two different
// commits produce different identifiers, so callers can tell them apart in the
// audit trail.
func TestCommitWorkingSetDistinctMessagesDistinctHashes(t *testing.T) {
	store := newTestStore(t)

	first, err := store.CommitWorkingSet("test: first")
	if err != nil {
		t.Fatalf("first CommitWorkingSet: %v", err)
	}
	second, err := store.CommitWorkingSet("test: second")
	if err != nil {
		t.Fatalf("second CommitWorkingSet: %v", err)
	}
	if first == second {
		t.Fatalf("expected distinct hashes for distinct commits, both were %q", first)
	}
}
