package knowledge

import (
	"sync"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestConcurrentPutAgainstSameRecordDoesNotFail proves the fix belongs at
// this layer, not just in internal/recipe's own callers: any caller of
// Store.Put (internal/recipe, internal/roles/*, internal/tasks,
// internal/mcpserver all call it directly) is equally exposed to Dolt's
// transient "1213 serialization failure" when two goroutines write the
// same row concurrently. Before this fix, only internal/recipe wrapped its
// own Put calls in a retry - every other caller had no protection at all.
func TestConcurrentPutAgainstSameRecordDoesNotFail(t *testing.T) {
	store := newTestStore(t)

	const goroutines = 8
	base := protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/racey",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Racey record",
		Source: protocol.KnowledgeRecordSource{
			Provider:    "jira",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State:      protocol.KnowledgeRecordValidityStateVerified,
			VerifiedBy: []string{"gareng"},
		},
	}
	if err := store.Put(base); err != nil {
		t.Fatalf("seed Put: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rec := base
			rec.Title = "Racey record (write)"
			errCh <- store.Put(rec)
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("concurrent Put: %v, want every concurrent writer to succeed via the transaction-conflict retry", err)
		}
	}

	got, err := store.Get(base.Id)
	if err != nil {
		t.Fatalf("Get after concurrent Put: %v", err)
	}
	if got.Title != "Racey record (write)" {
		t.Fatalf("Title = %q, want the last writer's value - not a half-written/corrupted record", got.Title)
	}
}

// TestConcurrentDeleteAndPutAgainstSameRecordDoesNotFail exercises Delete's
// identical transactional shape (also fixed alongside Put) against a
// concurrent Put on the same row.
func TestConcurrentDeleteAndPutAgainstSameRecordDoesNotFail(t *testing.T) {
	store := newTestStore(t)

	rec := protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/racey-delete",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Racey delete target",
		Source: protocol.KnowledgeRecordSource{
			Provider:    "jira",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State:      protocol.KnowledgeRecordValidityStateVerified,
			VerifiedBy: []string{"gareng"},
		},
	}
	if err := store.Put(rec); err != nil {
		t.Fatalf("seed Put: %v", err)
	}

	const goroutines = 8
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*2)
	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			errCh <- store.Put(rec)
		}()
		go func() {
			defer wg.Done()
			errCh <- store.Delete(rec.Id)
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("concurrent Put/Delete: %v, want the transaction-conflict retry to absorb transient failures", err)
		}
	}
}
