package adapters

import "testing"

func TestMatchJiraTransition(t *testing.T) {
	transitions := []JiraTransition{
		{ID: "11", Name: "Start Progress", ToStatusID: "3", ToStatusName: "In Progress"},
		{ID: "21", Name: "Done", ToStatusID: "10001", ToStatusName: "Done"},
	}

	match, available, ok := MatchJiraTransition(transitions, "done")
	if !ok {
		t.Fatal("expected a case-insensitive match on the target status name")
	}
	if match.ID != "21" {
		t.Errorf("match.ID = %q, want 21", match.ID)
	}
	if len(available) != 2 {
		t.Errorf("available = %v, want 2 entries", available)
	}

	if _, _, ok := MatchJiraTransition(transitions, "Cancelled"); ok {
		t.Error("expected no match for an unreachable status")
	}
}

func TestMatchAllJiraTransitions(t *testing.T) {
	transitions := []JiraTransition{
		{ID: "11", Name: "Start Progress", ToStatusID: "3", ToStatusName: "In Progress"},
		{ID: "21", Name: "Done", ToStatusID: "10001", ToStatusName: "Done"},
	}

	if matches := MatchAllJiraTransitions(transitions, "Cancelled"); len(matches) != 0 {
		t.Errorf("expected zero matches for an unreachable status, got %v", matches)
	}

	if matches := MatchAllJiraTransitions(transitions, "Done"); len(matches) != 1 || matches[0].ID != "21" {
		t.Errorf("expected exactly one match for Done, got %v", matches)
	}
}

// TestMatchAllJiraTransitions_Ambiguous guards the exact scenario a
// project-scoped TransitionPolicy resolution must detect: two different
// transitions on the same issue both reach (or are named) the configured
// target status, so picking either one silently would be a guess rather
// than a resolved decision.
func TestMatchAllJiraTransitions_Ambiguous(t *testing.T) {
	transitions := []JiraTransition{
		{ID: "31", Name: "Close as fixed", ToStatusID: "10001", ToStatusName: "Done"},
		{ID: "32", Name: "Done", ToStatusID: "10002", ToStatusName: "Closed"},
	}

	matches := MatchAllJiraTransitions(transitions, "Done")
	if len(matches) != 2 {
		t.Fatalf("expected both transitions to match (one by target status, one by transition name), got %v", matches)
	}
}
