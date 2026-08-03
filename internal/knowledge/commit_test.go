package knowledge

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestCommitWorkingSetCommitsPendingDeleteAndIsQueryableByHash(t *testing.T) {
	store := newTestStore(t)

	rec := protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/COMMIT-1",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Commit test fixture",
		Source: protocol.KnowledgeRecordSource{Provider: "test", RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{State: protocol.KnowledgeRecordValidityStateObserved},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Store.Open's own schema setup is itself uncommitted working-set state
	// (nothing before this feature ever committed anything), so there is no
	// ambient "before" commit to compare against - establish one explicitly
	// right after the Put, which is exactly this method's contract: callers
	// checkpoint whenever they want a revertable point.
	beforeHash, err := store.CommitWorkingSet("test: after put")
	if err != nil {
		t.Fatalf("CommitWorkingSet after put: %v", err)
	}
	if beforeHash == "" {
		t.Fatal("expected a non-empty commit hash after the initial schema+put")
	}

	if err := store.Delete(rec.Id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	hash, err := store.CommitWorkingSet("test: delete " + rec.Id)
	if err != nil {
		t.Fatalf("CommitWorkingSet: %v", err)
	}
	if hash == "" {
		t.Fatal("expected a non-empty commit hash after a real delete")
	}

	var message string
	if err := store.db.QueryRow(`SELECT message FROM dolt_log WHERE commit_hash = ?`, hash).Scan(&message); err != nil {
		t.Fatalf("query dolt_log for new commit: %v", err)
	}
	if message != "test: delete "+rec.Id {
		t.Fatalf("commit message = %q, want the message passed to CommitWorkingSet", message)
	}

	// The live table no longer has the record...
	if _, err := store.Get(rec.Id); err == nil {
		t.Fatal("expected the record to be gone from the live table")
	}
	// ...but it is still readable exactly as of the pre-delete commit,
	// without any checkout or mutation - the actual recovery path this
	// commit exists to guarantee.
	var count int
	query := `SELECT COUNT(*) FROM knowledge_records AS OF '` + beforeHash + `' WHERE id = ?`
	if err := store.db.QueryRow(query, rec.Id).Scan(&count); err != nil {
		t.Fatalf("AS OF query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the deleted record to still be readable AS OF the pre-delete commit, count=%d", count)
	}
}

func TestCommitWorkingSetIsANoOpWhenNothingIsPending(t *testing.T) {
	store := newTestStore(t)

	// Store.Open's own schema setup leaves pending working-set changes (it
	// does not commit either) - flush those first so this test asserts the
	// no-op behavior against a genuinely clean working set, not an
	// incidental first commit that happens to be the schema itself.
	if _, err := store.CommitWorkingSet("test: flush initial schema"); err != nil {
		t.Fatalf("flush initial schema: %v", err)
	}

	hash, err := store.CommitWorkingSet("test: should not happen")
	if err != nil {
		t.Fatalf("CommitWorkingSet on a clean working set should not error, got: %v", err)
	}
	if hash != "" {
		t.Fatalf("expected an empty hash when there was nothing to commit, got %q", hash)
	}
}
