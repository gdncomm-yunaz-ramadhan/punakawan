package capsule

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func newTestIndex(t *testing.T) *search.Index {
	t.Helper()
	ix, err := search.OpenIndex(filepath.Join(t.TempDir(), "bm25"))
	if err != nil {
		t.Fatalf("search.OpenIndex: %v", err)
	}
	t.Cleanup(func() {
		if err := ix.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return ix
}

func putAndIndex(t *testing.T, store *knowledge.Store, ix *search.Index, rec protocol.KnowledgeRecord) {
	t.Helper()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put %s: %v", rec.Id, err)
	}
	if err := ix.IndexRecord(knowledge.RecordWithUpdatedAt{Record: rec, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("IndexRecord %s: %v", rec.Id, err)
	}
}

func TestBuildFromRetrievalPopulatesRelevantKnowledgeWithReasons(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	rec := baseRecord("pkw:req/smoke/REQ-2", protocol.KnowledgeRecordTypeRequirement, "Refund settles same day for approved orders")
	putAndIndex(t, store, ix, rec)

	c, err := BuildFromRetrieval(context.Background(), store, ix, "cap-1", time.Now().UTC(), RetrievalInput{
		BuildInput: BuildInput{
			TaskID:    "bd-task-1",
			Role:      protocol.ContextCapsuleRolePetruk,
			Objective: "Implement the refund flow",
		},
		Query: "refund settles same day approved orders",
	})
	if err != nil {
		t.Fatalf("BuildFromRetrieval: %v", err)
	}
	if len(c.RelevantKnowledge) != 1 || c.RelevantKnowledge[0].Id != rec.Id {
		t.Fatalf("RelevantKnowledge = %+v, want %s retrieved", c.RelevantKnowledge, rec.Id)
	}
	if c.RelevantKnowledge[0].Reason == nil || *c.RelevantKnowledge[0].Reason == "" {
		t.Fatal("expected a non-empty reason on the retrieved knowledge reference")
	}
}

func TestBuildFromRetrievalExcludesForbiddenKnowledgeType(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	plan := baseRecord("pkw:petrukplan/smoke/PLAN-2", protocol.KnowledgeRecordTypePetrukPlan, "Petruk's refund plan")
	putAndIndex(t, store, ix, plan)

	c, err := BuildFromRetrieval(context.Background(), store, ix, "cap-1", time.Now().UTC(), RetrievalInput{
		BuildInput: BuildInput{
			TaskID:    "bd-task-1",
			Role:      protocol.ContextCapsuleRoleBagong,
			Objective: "Verify the refund flow",
		},
		Query: "Petruk's refund plan",
	})
	if err != nil {
		t.Fatalf("BuildFromRetrieval: %v", err)
	}
	for _, ref := range c.RelevantKnowledge {
		if ref.Id == plan.Id {
			t.Fatalf("RelevantKnowledge = %+v, want the petruk-plan record excluded from a bagong capsule", c.RelevantKnowledge)
		}
	}
}

func TestBuildFromRetrievalRespectsTokenBudget(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	for i := 0; i < 5; i++ {
		rec := baseRecord("pkw:req/smoke/REQ-budget-"+string(rune('a'+i)), protocol.KnowledgeRecordTypeRequirement, "Warehouse capacity threshold note")
		putAndIndex(t, store, ix, rec)
	}

	tiny := 1
	c, err := BuildFromRetrieval(context.Background(), store, ix, "cap-1", time.Now().UTC(), RetrievalInput{
		BuildInput: BuildInput{
			TaskID:      "bd-task-1",
			Role:        protocol.ContextCapsuleRolePetruk,
			Objective:   "Implement warehouse thresholds",
			TokenBudget: &tiny,
		},
		Query: "warehouse capacity threshold note",
	})
	if err != nil {
		t.Fatalf("BuildFromRetrieval: %v", err)
	}
	if len(c.RelevantKnowledge) != 0 {
		t.Fatalf("RelevantKnowledge = %+v, want none selected under a 1-token budget", c.RelevantKnowledge)
	}
}

func TestBuildFromRetrievalCombinesExplicitAndRetrievedKnowledge(t *testing.T) {
	store := newTestStore(t)
	ix := newTestIndex(t)

	manual := baseRecord("pkw:req/smoke/REQ-manual", protocol.KnowledgeRecordTypeRequirement, "Manually cited requirement")
	if err := store.Put(manual); err != nil {
		t.Fatalf("Put manual: %v", err)
	}
	retrieved := baseRecord("pkw:req/smoke/REQ-retrieved", protocol.KnowledgeRecordTypeRequirement, "Loyalty points expiry rule note")
	putAndIndex(t, store, ix, retrieved)

	c, err := BuildFromRetrieval(context.Background(), store, ix, "cap-1", time.Now().UTC(), RetrievalInput{
		BuildInput: BuildInput{
			TaskID:       "bd-task-1",
			Role:         protocol.ContextCapsuleRolePetruk,
			Objective:    "Implement loyalty points",
			KnowledgeIDs: []string{manual.Id},
		},
		Query: "loyalty points expiry rule note",
	})
	if err != nil {
		t.Fatalf("BuildFromRetrieval: %v", err)
	}
	seen := map[string]bool{}
	for _, ref := range c.RelevantKnowledge {
		seen[ref.Id] = true
	}
	if !seen[manual.Id] || !seen[retrieved.Id] {
		t.Fatalf("RelevantKnowledge = %+v, want both the manual and retrieved ids present", c.RelevantKnowledge)
	}
	for _, ref := range c.RelevantKnowledge {
		if ref.Id == manual.Id && ref.Reason != nil {
			t.Fatalf("manually-cited %s got a reason %q, want none", manual.Id, *ref.Reason)
		}
	}
}
