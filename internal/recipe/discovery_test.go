package recipe

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestDiscoveryHappyPathReachesExecutingOriginalTask(t *testing.T) {
	now := time.Now()
	s := NewDiscoverySession("disc-1", "jira.issue.search", "project.next-sprint.issues", "affiliate-platform")

	if err := s.Answer("project", "TRF", now); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if s.State != DiscoveryCollectingRules {
		t.Fatalf("State = %q, want collecting_rules", s.State)
	}

	if err := s.BeginCompiling(now); err != nil {
		t.Fatalf("BeginCompiling: %v", err)
	}
	if err := s.SetCandidate(&dummyRecipe, CompiledQuery{JQL: `project = "TRF"`}); err != nil {
		t.Fatalf("SetCandidate: %v", err)
	}
	if err := s.BeginTesting(now); err != nil {
		t.Fatalf("BeginTesting: %v", err)
	}
	if err := s.SetReport(passedReport(), now); err != nil {
		t.Fatalf("SetReport: %v", err)
	}
	if s.State != DiscoveryPresentingResults {
		t.Fatalf("State = %q, want presenting_results", s.State)
	}

	if err := s.Accept("user", now); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if err := s.Store(now); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := s.ResumeOriginalTask(now); err != nil {
		t.Fatalf("ResumeOriginalTask: %v", err)
	}
	if s.State != DiscoveryExecutingOriginalTask {
		t.Fatalf("State = %q, want executing_original_task", s.State)
	}
	if !s.State.Terminal() {
		t.Fatal("State.Terminal() = false, want true for executing_original_task")
	}
	if len(s.History) != 7 {
		t.Fatalf("History has %d entries, want 7", len(s.History))
	}
}

func TestDiscoveryCorrectionLoopCanRepeat(t *testing.T) {
	now := time.Now()
	s := discoverySessionAtPresentingResults(t, now)

	for i := 0; i < 3; i++ {
		if err := s.Correct(now); err != nil {
			t.Fatalf("Correct (iteration %d): %v", i, err)
		}
		s.Exclude("TRF-1")
		if err := s.BeginRetesting(now); err != nil {
			t.Fatalf("BeginRetesting (iteration %d): %v", i, err)
		}
		if err := s.SetCandidate(&dummyRecipe, CompiledQuery{JQL: `project = "TRF"`}); err != nil {
			t.Fatalf("SetCandidate (iteration %d): %v", i, err)
		}
		if err := s.SetReport(passedReport(), now); err != nil {
			t.Fatalf("SetReport (iteration %d): %v", i, err)
		}
		if s.State != DiscoveryPresentingResults {
			t.Fatalf("State after iteration %d = %q, want presenting_results", i, s.State)
		}
	}
	if len(s.Exclusions) != 3 {
		t.Fatalf("Exclusions = %v, want 3 entries (one per correction)", s.Exclusions)
	}
}

func TestDiscoveryAcceptRejectsUnpassedReport(t *testing.T) {
	now := time.Now()
	s := discoverySessionAtPresentingResults(t, now)
	s.LastReport = &ValidationReport{Status: "failed", FailureReason: "no results"}

	if err := s.Accept("user", now); err == nil {
		t.Fatal("Accept: want error for a non-passed report, got nil")
	}
}

func TestDiscoveryRejectsInvalidTransition(t *testing.T) {
	s := NewDiscoverySession("disc-2", "jira.issue.search", "", "affiliate-platform")
	if err := s.transition(DiscoveryPresentingResults, "skip ahead", time.Now()); err == nil {
		t.Fatal("transition: want error jumping from missing straight to presenting_results")
	}
}

func TestDiscoverySaveAsDraftFromCorrecting(t *testing.T) {
	now := time.Now()
	s := discoverySessionAtPresentingResults(t, now)
	if err := s.Correct(now); err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if err := s.SaveAsDraft("user wants to keep tweaking later", now); err != nil {
		t.Fatalf("SaveAsDraft: %v", err)
	}
	if s.State != DiscoverySavedAsDraft {
		t.Fatalf("State = %q, want saved_as_draft", s.State)
	}
	if !s.State.Terminal() {
		t.Fatal("State.Terminal() = false, want true for saved_as_draft")
	}
}

func TestDiscoveryMissingConstraints(t *testing.T) {
	now := time.Now()
	s := NewDiscoverySession("disc-3", "jira.issue.search", "", "affiliate-platform")
	if err := s.Answer("project", "TRF", now); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	missing := s.MissingConstraints([]string{"project", "board", "component"})
	if len(missing) != 2 || missing[0] != "board" || missing[1] != "component" {
		t.Fatalf("MissingConstraints = %v, want [board component]", missing)
	}
}

var dummyRecipe protocol.KnowledgeRecordRetrievalRecipe

func passedReport() ValidationReport {
	return ValidationReport{Status: protocol.KnowledgeRecordRetrievalRecipeValidationStatusPassed}
}

func discoverySessionAtPresentingResults(t *testing.T, now time.Time) *DiscoverySession {
	t.Helper()
	s := NewDiscoverySession("disc-fixture", "jira.issue.search", "project.next-sprint.issues", "affiliate-platform")
	if err := s.Answer("project", "TRF", now); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if err := s.BeginCompiling(now); err != nil {
		t.Fatalf("BeginCompiling: %v", err)
	}
	if err := s.SetCandidate(&dummyRecipe, CompiledQuery{JQL: `project = "TRF"`}); err != nil {
		t.Fatalf("SetCandidate: %v", err)
	}
	if err := s.BeginTesting(now); err != nil {
		t.Fatalf("BeginTesting: %v", err)
	}
	if err := s.SetReport(passedReport(), now); err != nil {
		t.Fatalf("SetReport: %v", err)
	}
	return s
}
