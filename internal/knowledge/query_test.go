package knowledge

import (
	"context"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// putRecord builds a valid record with the given id/type/validity/scope/source
// and Puts it, failing the test on error.
func putRecord(t *testing.T, s *Store, id string, typ protocol.KnowledgeRecordType, state protocol.KnowledgeRecordValidityState, repo, provider string) {
	t.Helper()
	rec := protocol.KnowledgeRecord{
		Id:     id,
		Type:   typ,
		Status: "active",
		Title:  "record " + id,
		Source: protocol.KnowledgeRecordSource{Provider: provider, RetrievedAt: time.Now().UTC()},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{State: state},
	}
	if state == protocol.KnowledgeRecordValidityStateVerified {
		rec.Validity.VerifiedBy = []string{"test"}
	}
	if repo != "" {
		rec.Scope = &protocol.KnowledgeRecordScope{Repository: &repo}
	}
	if err := s.Put(rec); err != nil {
		t.Fatalf("Put %s: %v", id, err)
	}
}

func TestCount(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if n, err := store.Count(ctx); err != nil || n != 0 {
		t.Fatalf("Count on empty store = (%d, %v), want (0, nil)", n, err)
	}

	putRecord(t, store, "pkw:requirement/repo-a/one", protocol.KnowledgeRecordTypeRequirement, protocol.KnowledgeRecordValidityStateInferred, "repo-a", "manual")
	putRecord(t, store, "pkw:requirement/repo-a/two", protocol.KnowledgeRecordTypeRequirement, protocol.KnowledgeRecordValidityStateInferred, "repo-a", "manual")

	n, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 2 {
		t.Fatalf("Count = %d, want 2", n)
	}
}

func TestListRecordsFiltering(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	putRecord(t, store, "pkw:requirement/repo-a/req", protocol.KnowledgeRecordTypeRequirement, protocol.KnowledgeRecordValidityStateVerified, "repo-a", "manual")
	putRecord(t, store, "pkw:decision/repo-b/dec", protocol.KnowledgeRecordTypeDecision, protocol.KnowledgeRecordValidityStateStale, "repo-b", "jira")

	// No filter: everything.
	all, next, err := store.ListRecords(ctx, KnowledgeListQuery{})
	if err != nil {
		t.Fatalf("ListRecords (no filter): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListRecords (no filter) = %d records, want 2", len(all))
	}
	if next != "" {
		t.Fatalf("nextCursor = %q, want empty for an unlimited query", next)
	}

	// Type filter (indexed column).
	byType, _, err := store.ListRecords(ctx, KnowledgeListQuery{Type: string(protocol.KnowledgeRecordTypeDecision)})
	if err != nil {
		t.Fatalf("ListRecords (type): %v", err)
	}
	if len(byType) != 1 || byType[0].Id != "pkw:decision/repo-b/dec" {
		t.Fatalf("ListRecords (type=decision) = %+v, want only the decision record", byType)
	}

	// Validity state filter.
	byState, _, err := store.ListRecords(ctx, KnowledgeListQuery{ValidityState: string(protocol.KnowledgeRecordValidityStateStale)})
	if err != nil {
		t.Fatalf("ListRecords (state): %v", err)
	}
	if len(byState) != 1 || byState[0].Id != "pkw:decision/repo-b/dec" {
		t.Fatalf("ListRecords (state=stale) = %+v, want only the stale record", byState)
	}

	// Repository filter (JSON path).
	byRepo, _, err := store.ListRecords(ctx, KnowledgeListQuery{Repository: "repo-a"})
	if err != nil {
		t.Fatalf("ListRecords (repository): %v", err)
	}
	if len(byRepo) != 1 || byRepo[0].Id != "pkw:requirement/repo-a/req" {
		t.Fatalf("ListRecords (repository=repo-a) = %+v, want only the repo-a record", byRepo)
	}

	// Source filter (JSON path).
	bySource, _, err := store.ListRecords(ctx, KnowledgeListQuery{Source: "jira"})
	if err != nil {
		t.Fatalf("ListRecords (source): %v", err)
	}
	if len(bySource) != 1 || bySource[0].Id != "pkw:decision/repo-b/dec" {
		t.Fatalf("ListRecords (source=jira) = %+v, want only the jira record", bySource)
	}

	// A filter matching nothing returns an empty (non-error) result.
	none, _, err := store.ListRecords(ctx, KnowledgeListQuery{Repository: "no-such-repo"})
	if err != nil {
		t.Fatalf("ListRecords (unmatched): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("ListRecords (unmatched) = %+v, want none", none)
	}
}

func TestListRecordsCursorPagination(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	const total = 5
	for i := 0; i < total; i++ {
		id := "pkw:requirement/repo-a/rec-" + string(rune('a'+i))
		putRecord(t, store, id, protocol.KnowledgeRecordTypeRequirement, protocol.KnowledgeRecordValidityStateInferred, "repo-a", "manual")
	}

	// Page through 2 at a time, collecting ids, and assert no duplicates,
	// full coverage, and a terminating empty cursor.
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		page, next, err := store.ListRecords(ctx, KnowledgeListQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("ListRecords page %d: %v", pages, err)
		}
		pages++
		if len(page) > 2 {
			t.Fatalf("page %d returned %d records, want <= Limit (2)", pages, len(page))
		}
		for _, rec := range page {
			if seen[rec.Id] {
				t.Fatalf("record %s returned on more than one page", rec.Id)
			}
			seen[rec.Id] = true
		}
		if next == "" {
			break
		}
		cursor = next
		if pages > total+2 {
			t.Fatal("pagination did not terminate")
		}
	}

	if len(seen) != total {
		t.Fatalf("paginated over %d distinct records, want %d", len(seen), total)
	}
	// 5 records at 2/page => pages of 2, 2, 1 (the last has no +1 overflow row
	// so its cursor is empty): 3 pages.
	if pages != 3 {
		t.Fatalf("paginated in %d pages, want 3", pages)
	}
}

func TestListRecordsRejectsBadCursor(t *testing.T) {
	store := newTestStore(t)
	if _, _, err := store.ListRecords(context.Background(), KnowledgeListQuery{Cursor: "!!!not-base64!!!"}); err == nil {
		t.Fatal("expected an error for a malformed cursor")
	}
}
