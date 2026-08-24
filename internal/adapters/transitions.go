package adapters

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JiraTransition is one entry of an "atlassian.getTransitionsForJiraIssue"
// adapter call's result: a workflow transition currently available on an
// issue, and the status it lands the issue on.
type JiraTransition struct {
	ID           string
	Name         string
	ToStatusID   string
	ToStatusName string
}

// DecodeJiraTransitions parses the raw JSON result of an
// "atlassian.getTransitionsForJiraIssue" adapter call into its list of
// currently available transitions.
func DecodeJiraTransitions(raw json.RawMessage) ([]JiraTransition, error) {
	var result struct {
		Transitions []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			ToStatus struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"toStatus"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("adapters: decode available transitions: %w", err)
	}
	out := make([]JiraTransition, 0, len(result.Transitions))
	for _, t := range result.Transitions {
		out = append(out, JiraTransition{ID: t.ID, Name: t.Name, ToStatusID: t.ToStatus.ID, ToStatusName: t.ToStatus.Name})
	}
	return out, nil
}

// MatchJiraTransition finds the transition among transitions whose target
// status name matches targetStatusName case-insensitively, falling back to
// matching the transition's own name - some Jira workflows name a
// transition differently from the status it lands on, so a caller asking
// to reach a named status should still find it under either name. ok is
// false when nothing matches; available lists every reachable target
// status name in transitions' order, so a caller can report back what it
// could have picked instead.
func MatchJiraTransition(transitions []JiraTransition, targetStatusName string) (match JiraTransition, available []string, ok bool) {
	for _, t := range transitions {
		available = append(available, t.ToStatusName)
		if strings.EqualFold(t.ToStatusName, targetStatusName) || strings.EqualFold(t.Name, targetStatusName) {
			return t, available, true
		}
	}
	return JiraTransition{}, available, false
}
