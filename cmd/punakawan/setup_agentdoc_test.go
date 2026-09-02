package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAgentDocSectionUpdatesInPlace covers the property that makes this
// safe to re-run: the section is rewritten between its own markers, so
// updating the guidance never disturbs, duplicates, or truncates what a
// human wrote around it.
func TestAgentDocSectionUpdatesInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	original := "# House rules\n\nRun the tests before pushing.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	changed, err := ensureAgentDocSection(path)
	if err != nil || !changed {
		t.Fatalf("ensureAgentDocSection = %v, %v; want it to add the section", changed, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.HasPrefix(string(after), original) {
		t.Fatalf("the existing content was disturbed:\n%s", after)
	}
	if !strings.Contains(string(after), "start_delivery") {
		t.Fatalf("the section is missing from:\n%s", after)
	}

	changed, err = ensureAgentDocSection(path)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if changed {
		t.Fatal("second run reported a change; an unchanged section must stay quiet")
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Count(string(again), agentDocBeginMarker) != 1 {
		t.Fatalf("the section was appended twice:\n%s", again)
	}
}

// TestAgentDocSectionRefusesAnUnterminatedBlock covers the one case where
// rewriting would destroy work: a hand-edited block with no end marker.
func TestAgentDocSectionRefusesAnUnterminatedBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	body := agentDocBeginMarker + "\nhand-edited\n\n# Everything after this must survive\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	if _, err := ensureAgentDocSection(path); err == nil {
		t.Fatal("expected an error for an unterminated block")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(after) != body {
		t.Fatalf("the file was modified despite the error:\n%s", after)
	}
}

// TestAgentDocTargetsFollowTheRepo checks the file choice: every
// instruction file a repo already keeps, or AGENTS.md when it keeps none.
func TestAgentDocTargetsFollowTheRepo(t *testing.T) {
	empty := t.TempDir()
	if got := agentDocTargets(empty); len(got) != 1 || got[0] != "AGENTS.md" {
		t.Fatalf("agentDocTargets on an empty repo = %v, want [AGENTS.md]", got)
	}

	both := t.TempDir()
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if err := os.WriteFile(filepath.Join(both, name), []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if got := agentDocTargets(both); len(got) != 2 {
		t.Fatalf("agentDocTargets = %v, want both files a repo already keeps", got)
	}
}
