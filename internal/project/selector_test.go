package project

import (
	"reflect"
	"testing"
)

func sampleProject() Project {
	return Project{
		ID: "p",
		Metadata: []MetadataEntry{
			{Key: "jira.project_key", Description: "d", Value: "TRF"},
			{Key: "jira.board_id", Description: "d", Value: 127},
			{Key: "team.owner", Description: "d", Value: "AFF"},
			{Key: "build.command", Description: "d", Value: "make"},
			{Key: "test.command", Description: "d", Value: "make test"},
		},
	}
}

func keysOf(entries []MetadataEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Key
	}
	return out
}

func TestSelectorExplicitKeysFirst(t *testing.T) {
	p := sampleProject()
	got := PrioritySelector{}.Select(p, "", "", []string{"test.command", "team.owner"})
	// Explicit requested keys come first in request order, then general fill.
	if got[0].Key != "test.command" || got[1].Key != "team.owner" {
		t.Fatalf("explicit keys not first: %v", keysOf(got))
	}
}

func TestSelectorCapabilityNamespace(t *testing.T) {
	p := sampleProject()
	got := PrioritySelector{}.Select(p, "jira", "", nil)
	// Both jira.* keys must appear before any non-jira key.
	if got[0].Key != "jira.project_key" || got[1].Key != "jira.board_id" {
		t.Fatalf("capability namespace not prioritized: %v", keysOf(got))
	}
}

func TestSelectorIntentExactMatch(t *testing.T) {
	p := sampleProject()
	got := PrioritySelector{}.Select(p, "", "build.command", nil)
	if got[0].Key != "build.command" {
		t.Fatalf("intent match not first: %v", keysOf(got))
	}
}

func TestSelectorDeterministicAndDeduped(t *testing.T) {
	p := sampleProject()
	a := PrioritySelector{}.Select(p, "jira", "team.owner", []string{"jira.project_key"})
	b := PrioritySelector{}.Select(p, "jira", "team.owner", []string{"jira.project_key"})
	if !reflect.DeepEqual(keysOf(a), keysOf(b)) {
		t.Fatalf("non-deterministic: %v vs %v", keysOf(a), keysOf(b))
	}
	// No duplicates even though jira.project_key is both requested and in the
	// jira namespace.
	seen := map[string]bool{}
	for _, e := range a {
		if seen[e.Key] {
			t.Fatalf("duplicate key %q in %v", e.Key, keysOf(a))
		}
		seen[e.Key] = true
	}
}

func TestSelectorRespectsLimit(t *testing.T) {
	p := sampleProject()
	got := PrioritySelector{Limit: 2}.Select(p, "", "", nil)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (limit)", len(got))
	}
}
