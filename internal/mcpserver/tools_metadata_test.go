package mcpserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/project"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

// TestSetProjectMetadataClosesPrepareWorkContextGap proves end to end that
// a workflow with a required metadata key an agent can auto-detect resolves
// without a human editing
// project.yaml, and re-running prepare_work_context after set_project_metadata
// no longer reports that key as missing.
func TestSetProjectMetadataClosesPrepareWorkContextGap(t *testing.T) {
	a := newTestApp(t)
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "go.mod"), []byte("module example.com/fixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	store, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(workflowdef.Definition{
		Version:          workflowdef.SchemaVersion,
		ID:               "implement",
		Name:             "Implementation with Tests",
		Enabled:          true,
		RequiredMetadata: []string{"test.command"},
	}); err != nil {
		t.Fatalf("save definition: %v", err)
	}

	prepare := prepareWorkContextHandler(a)
	_, before, err := prepare(context.Background(), nil, PrepareWorkContextInput{WorkflowId: "implement", Objective: "implement thing"})
	if err != nil {
		t.Fatalf("prepare_work_context (before): %v", err)
	}
	if len(before.Missing) != 1 || before.Missing[0].Key != "test.command" || before.State != "awaiting-clarification" {
		t.Fatalf("expected test.command to be missing before metadata is set, got %+v", before)
	}

	setMetadata := setProjectMetadataHandler(a)
	if _, _, err := setMetadata(context.Background(), nil, SetProjectMetadataInput{Key: "test.command"}); err != nil {
		t.Fatalf("set_project_metadata: %v", err)
	}

	_, after, err := prepare(context.Background(), nil, PrepareWorkContextInput{WorkflowId: "implement", Objective: "implement thing"})
	if err != nil {
		t.Fatalf("prepare_work_context (after): %v", err)
	}
	if len(after.Missing) != 0 {
		t.Fatalf("expected no missing metadata after set_project_metadata, got %+v", after.Missing)
	}
	if after.State == "awaiting-clarification" {
		t.Fatalf("expected run to leave awaiting-clarification once metadata is supplied, got state %q", after.State)
	}
}

func TestSetProjectMetadataCreatesNewKey(t *testing.T) {
	a := newTestApp(t)
	h := setProjectMetadataHandler(a)

	_, out, err := h(context.Background(), nil, SetProjectMetadataInput{
		Key:         "jira.project_key",
		Value:       "TRF",
		Description: "Jira project key used for this project.",
	})
	if err != nil {
		t.Fatalf("set_project_metadata: %v", err)
	}
	if out.Action != "created" || out.Revision != 1 || out.AutoDetected {
		t.Fatalf("unexpected output: %+v", out)
	}

	p, err := project.Load(a.Workspace.Root)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	entry, ok := p.MetadataFor("jira.project_key")
	if !ok || entry.Value != "TRF" {
		t.Fatalf("expected persisted entry, got %+v (ok=%v)", entry, ok)
	}
}

func TestSetProjectMetadataUpdatesExistingKeyAndPreservesDescriptionWhenOmitted(t *testing.T) {
	a := newTestApp(t)
	h := setProjectMetadataHandler(a)

	if _, _, err := h(context.Background(), nil, SetProjectMetadataInput{
		Key:         "test.command",
		Value:       "make test",
		Description: "Command used to run this project's test suite.",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, out, err := h(context.Background(), nil, SetProjectMetadataInput{
		Key:   "test.command",
		Value: "go test ./...",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if out.Action != "updated" || out.Revision != 2 {
		t.Fatalf("unexpected output: %+v", out)
	}
	if out.Description != "Command used to run this project's test suite." {
		t.Fatalf("expected description preserved when omitted, got %q", out.Description)
	}

	p, err := project.Load(a.Workspace.Root)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	entry, ok := p.MetadataFor("test.command")
	if !ok || entry.Value != "go test ./..." {
		t.Fatalf("expected updated value persisted, got %+v (ok=%v)", entry, ok)
	}
}

func TestSetProjectMetadataAutoDetectsTestCommandFromGoMod(t *testing.T) {
	a := newTestApp(t)
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "go.mod"), []byte("module example.com/fixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	h := setProjectMetadataHandler(a)
	_, out, err := h(context.Background(), nil, SetProjectMetadataInput{Key: "test.command"})
	if err != nil {
		t.Fatalf("set_project_metadata: %v", err)
	}
	if !out.AutoDetected {
		t.Fatalf("expected auto_detected=true, got %+v", out)
	}
	if out.Value != "go test ./..." {
		t.Fatalf("expected auto-detected go test command, got %v", out.Value)
	}
	if out.Description == "" {
		t.Fatalf("expected an auto-generated description for a newly created key, got empty")
	}
}

func TestSetProjectMetadataAutoDetectPrefersMakefileOverGoMod(t *testing.T) {
	a := newTestApp(t)
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "go.mod"), []byte("module example.com/fixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(a.Workspace.Root, "Makefile"), []byte("test:\n\tgo test ./...\n"), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	h := setProjectMetadataHandler(a)
	_, out, err := h(context.Background(), nil, SetProjectMetadataInput{Key: "test.command"})
	if err != nil {
		t.Fatalf("set_project_metadata: %v", err)
	}
	if out.Value != "make test" {
		t.Fatalf("expected Makefile target to win, got %v", out.Value)
	}
}

func TestSetProjectMetadataFailsCleanlyWhenAutoDetectFindsNothing(t *testing.T) {
	a := newTestApp(t)
	h := setProjectMetadataHandler(a)

	_, _, err := h(context.Background(), nil, SetProjectMetadataInput{Key: "test.command"})
	if err == nil {
		t.Fatal("expected an error when nothing in the workspace root is auto-detectable")
	}

	p, loadErr := project.Load(a.Workspace.Root)
	if loadErr != nil {
		t.Fatalf("load project: %v", loadErr)
	}
	if _, ok := p.MetadataFor("test.command"); ok {
		t.Fatal("a failed auto-detect must not leave a partial metadata entry behind")
	}
}

func TestSetProjectMetadataRejectsUnknownKeyWithoutExplicitValue(t *testing.T) {
	a := newTestApp(t)
	h := setProjectMetadataHandler(a)

	_, _, err := h(context.Background(), nil, SetProjectMetadataInput{Key: "some.unrecognized.key"})
	if err == nil {
		t.Fatal("expected an error when value is omitted for a key with no detector")
	}
}

func TestSetProjectMetadataRequiresDescriptionForNewUndetectableKey(t *testing.T) {
	a := newTestApp(t)
	h := setProjectMetadataHandler(a)

	_, _, err := h(context.Background(), nil, SetProjectMetadataInput{
		Key:   "custom.thing",
		Value: "some-value",
	})
	if err == nil || !errors.Is(err, project.ErrMissingField) {
		t.Fatalf("expected ErrMissingField, got %v", err)
	}
}

func TestSetProjectMetadataRejectsSecretLikeKey(t *testing.T) {
	a := newTestApp(t)
	h := setProjectMetadataHandler(a)

	_, _, err := h(context.Background(), nil, SetProjectMetadataInput{
		Key:         "jira.api_token",
		Value:       "shh",
		Description: "should be rejected",
	})
	if err == nil || !errors.Is(err, project.ErrSecretRejected) {
		t.Fatalf("expected ErrSecretRejected, got %v", err)
	}
}

func TestSetProjectMetadataRequiresKey(t *testing.T) {
	a := newTestApp(t)
	h := setProjectMetadataHandler(a)

	_, _, err := h(context.Background(), nil, SetProjectMetadataInput{Value: "x"})
	if err == nil {
		t.Fatal("expected an error when key is empty")
	}
}
