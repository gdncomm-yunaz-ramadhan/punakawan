package dossier

import (
	"context"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func sp(s string) *string { return &s }

func decisionRecord(id, title string, repo *string) protocol.KnowledgeRecord {
	rec := protocol.KnowledgeRecord{
		Id:     id,
		Type:   protocol.KnowledgeRecordTypeDecision,
		Status: "active",
		Title:  title,
		Source: protocol.KnowledgeRecordSource{
			Provider:    "punakawan",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateObserved,
		},
	}
	if repo != nil {
		rec.Scope = &protocol.KnowledgeRecordScope{Repository: repo}
	}
	return rec
}

// TestBuildFiltersDecisionsToAffectedRepos covers punokawan-nmw: a decision
// scoped to a repository outside the dossier's affected repositories is
// dropped, while an in-repo (and an unscoped) decision are kept.
func TestBuildFiltersDecisionsToAffectedRepos(t *testing.T) {
	ws, sup := newTestWorkspace(t) // declares repo-a and repo-b
	store := newTestStore(t)

	inRepo := decisionRecord("pkw:decision/test-ws/DEC-IN", "Decision in repo-a", sp("repo-a"))
	if err := store.Put(inRepo); err != nil {
		t.Fatalf("seed in-repo decision Put: %v", err)
	}
	unscoped := decisionRecord("pkw:decision/test-ws/DEC-GLOBAL", "Cross-cutting decision", nil)
	if err := store.Put(unscoped); err != nil {
		t.Fatalf("seed unscoped decision Put: %v", err)
	}
	outRepo := decisionRecord("pkw:decision/test-ws/DEC-OUT", "Decision in repo-z", sp("repo-z"))
	if err := store.Put(outRepo); err != nil {
		t.Fatalf("seed out-of-repo decision Put: %v", err)
	}

	rec, err := Build(context.Background(), ws, sup, store, BuildInput{WorkspaceID: ws.ID, RunID: "run-scope"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := knowledge.Validate(rec); err != nil {
		t.Fatalf("Validate(rec): %v", err)
	}

	decs := rec.ContextDossier.RelevantPreviousDecisions
	if !hasSummary(decs, inRepo) {
		t.Fatalf("RelevantPreviousDecisions = %v, want the repo-a decision present", decs)
	}
	if !hasSummary(decs, unscoped) {
		t.Fatalf("RelevantPreviousDecisions = %v, want the unscoped decision present", decs)
	}
	if hasSummary(decs, outRepo) {
		t.Fatalf("RelevantPreviousDecisions = %v, want the repo-z decision filtered out", decs)
	}
}

func hasSummary(list []string, rec protocol.KnowledgeRecord) bool {
	want := summarize(rec)
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
