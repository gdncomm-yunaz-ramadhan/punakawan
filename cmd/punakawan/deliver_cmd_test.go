package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeliverFromReferenceFlagsWorksOutsideAnyProject covers AC6:
// `deliver` must succeed with no workspace.yaml and no .git directory
// anywhere above the working directory, unlike loadApp-backed commands
// (workspace/git/worktree), since bootstrapping a brand new delivery is
// exactly the case where no project checkout exists yet.
func TestDeliverFromReferenceFlagsWorksOutsideAnyProject(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	dir := t.TempDir()

	out, err := runCLI(t, dir, "deliver", "--reference", "PAY-1842", "--url", "https://example.com/spec", "--text", "an ambiguous note")
	if err != nil {
		t.Fatalf("deliver: %v\n%s", err, out)
	}
	if !strings.Contains(out, "orchestration:") {
		t.Fatalf("output = %q, want it to report the orchestration id", out)
	}
	if !strings.Contains(out, "pending questions:") || !strings.Contains(out, "an ambiguous note") {
		t.Fatalf("output = %q, want the ambiguous reference listed as a pending question", out)
	}
}

// TestDeliverFromFile covers the --file JSON input path with a
// reference that classifies confidently, so no pending question should
// appear in the output.
func TestDeliverFromFile(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	dir := t.TempDir()

	file := filepath.Join(dir, "refs.json")
	data, err := json.Marshal(map[string]any{"references": []string{"acme/checkout#42"}})
	if err != nil {
		t.Fatalf("marshal refs.json: %v", err)
	}
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatalf("write refs.json: %v", err)
	}

	out, err := runCLI(t, dir, "deliver", "--file", file)
	if err != nil {
		t.Fatalf("deliver: %v\n%s", err, out)
	}
	if !strings.Contains(out, "orchestration:") {
		t.Fatalf("output = %q, want it to report the orchestration id", out)
	}
	if strings.Contains(out, "pending questions:") {
		t.Fatalf("output = %q, want no pending questions for a confidently classified github reference", out)
	}
}

// TestDeliverPositionalArgsAndRepeatedRetryReuseOrchestration covers
// plain positional references plus StartDelivery's own idempotency
// contract from the CLI side: two calls that both omit an idempotency
// key are two distinct orchestrations (the CLI never supplies one), so
// this only guards that a positional-arg call succeeds at all.
func TestDeliverPositionalArgs(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	dir := t.TempDir()

	out, err := runCLI(t, dir, "deliver", "PAY-1", "PAY-2")
	if err != nil {
		t.Fatalf("deliver: %v\n%s", err, out)
	}
	if !strings.Contains(out, "orchestration:") {
		t.Fatalf("output = %q, want it to report the orchestration id", out)
	}
}

func TestDeliverRequiresAtLeastOneReference(t *testing.T) {
	t.Setenv("PUNAKAWAN_DATA_DIR", t.TempDir())
	dir := t.TempDir()

	if _, err := runCLI(t, dir, "deliver"); err == nil {
		t.Fatal("expected an error when no reference is given")
	}
}
