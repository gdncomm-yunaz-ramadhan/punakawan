package taskcontext

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func sp(s string) *string { return &s }

// TestBuildFiltersOutOfScopeContracts covers punokawan-d9v: source excerpts
// are scoped to the parent requirement rather than dumping every contract in
// the store.
func TestBuildFiltersOutOfScopeContracts(t *testing.T) {
	store := newTestStore(t)

	req := baseRecord("pkw:requirement/test-ws/REQ-SCOPE", protocol.KnowledgeRecordTypeRequirement, "Scoped requirement")
	req.Scope = &protocol.KnowledgeRecordScope{Repository: sp("repo-x")}
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	inScope := baseRecord("pkw:apicontract/test-ws/AC-IN", protocol.KnowledgeRecordTypeApiContract, "In-scope contract")
	inScope.Scope = &protocol.KnowledgeRecordScope{Repository: sp("repo-x")}
	if err := store.Put(inScope); err != nil {
		t.Fatalf("seed in-scope contract Put: %v", err)
	}

	outScope := baseRecord("pkw:apicontract/test-ws/AC-OUT", protocol.KnowledgeRecordTypeApiContract, "Out-of-scope contract")
	outScope.Scope = &protocol.KnowledgeRecordScope{Repository: sp("repo-y")}
	if err := store.Put(outScope); err != nil {
		t.Fatalf("seed out-of-scope contract Put: %v", err)
	}

	got, err := Build(context.Background(), store, BuildInput{TaskID: "bd-scope-1", RequirementID: req.Id})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !containsString(got.RelevantSourceExcerpts, summarize(inScope)) {
		t.Fatalf("RelevantSourceExcerpts = %v, want the in-scope contract present", got.RelevantSourceExcerpts)
	}
	if containsString(got.RelevantSourceExcerpts, summarize(outScope)) {
		t.Fatalf("RelevantSourceExcerpts = %v, want the out-of-scope (repo-y) contract filtered out", got.RelevantSourceExcerpts)
	}
}

// TestBuildCapsRelatedDecisions covers the count cap from punokawan-d9v.
func TestBuildCapsRelatedDecisions(t *testing.T) {
	store := newTestStore(t)

	req := baseRecord("pkw:requirement/test-ws/REQ-CAP", protocol.KnowledgeRecordTypeRequirement, "Cap requirement")
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement Put: %v", err)
	}

	for i := 0; i < maxRelatedDecisions+5; i++ {
		d := baseRecord("pkw:decision/test-ws/DEC-"+itoa(i), protocol.KnowledgeRecordTypeDecision, "Decision "+itoa(i))
		if err := store.Put(d); err != nil {
			t.Fatalf("seed decision %d Put: %v", i, err)
		}
	}

	got, err := Build(context.Background(), store, BuildInput{TaskID: "bd-cap-1", RequirementID: req.Id})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(got.RelatedDecisions) > maxRelatedDecisions {
		t.Fatalf("RelatedDecisions has %d entries, want at most the cap %d", len(got.RelatedDecisions), maxRelatedDecisions)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
