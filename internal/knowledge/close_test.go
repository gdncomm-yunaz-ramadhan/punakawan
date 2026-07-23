package knowledge

import (
	"sync"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestCloseStopsServerDespiteConcurrentConnections is the regression test
// for the bug punokawan-q9r.6.1's investigation surfaced but left
// unfixed: Close's "is another process still using this shared server"
// check counts PROCESSLIST rows other than its own query's connection.
// Before this fix, database/sql's default unlimited connection pool let
// concurrent Put calls open a second physical connection from this SAME
// process, which that check couldn't distinguish from a genuinely
// different process - so Close silently left the server running forever,
// with nothing left to ever stop it once this process exits. Forcing
// SetMaxOpenConns(1) in waitForConnection makes the check correct: this
// process can never itself hold more than one connection, so any other
// PROCESSLIST row really is a different process.
func TestCloseStopsServerDespiteConcurrentConnections(t *testing.T) {
	store := newTestStore(t)

	// Drive genuine concurrent traffic through the store first - this is
	// exactly the condition that used to open a second pooled connection
	// under the old unlimited-pool default.
	const goroutines = 8
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rec := protocol.KnowledgeRecord{
				Id:     "pkw:req/fixture/close-race",
				Type:   protocol.KnowledgeRecordTypeRequirement,
				Status: "active",
				Title:  "Close race fixture",
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
			_ = store.Put(rec)
		}(i)
	}
	wg.Wait()

	if store.db.Stats().MaxOpenConnections != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1 (Close's other-process check depends on this)", store.db.Stats().MaxOpenConnections)
	}

	server := store.server
	if server == nil {
		t.Skip("no owned server on this Store (reused an existing one) - nothing to verify")
	}

	// newTestStore already registers t.Cleanup(store.Close) - call it here
	// too (idempotent-enough for this assertion: Close only stops the
	// server once) so this test observes the actual exit within its own
	// body rather than relying on cleanup ordering.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-server.Done():
	case <-time.After(6 * time.Second):
		t.Fatal("dolt sql-server still running after Close - Close's other-process heuristic likely misfired")
	}
}
