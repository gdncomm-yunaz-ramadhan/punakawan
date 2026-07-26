package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

func TestProposeProjectLearningCreatesAndDedups(t *testing.T) {
	a := newTestApp(t)
	store, _ := workflowdef.Open(a.Workspace.Root)
	if _, err := store.Save(workflowdef.Definition{Version: workflowdef.SchemaVersion, ID: "wf", Name: "v1", Enabled: false}); err != nil {
		t.Fatal(err)
	}

	candidate := map[string]any{
		"version": workflowdef.SchemaVersion,
		"id":      "wf",
		"name":    "v2 (improved)",
		"enabled": true, // must be ignored on acceptance
	}
	h := proposeProjectLearningHandler(a)
	_, out, err := h(context.Background(), nil, ProposeProjectLearningInput{
		ArtifactType: "workflow", TargetId: "wf", Candidate: candidate,
		Rationale: "the last three runs revised this the same way", SourceRunIds: []string{"run-1"}, EvidenceIds: []string{"ev-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Deduplicated || out.SupportCount != 1 || out.ReviewId == "" {
		t.Fatalf("first proposal: %+v", out)
	}

	// Canonical state must be UNCHANGED before acceptance.
	def, _ := store.Get("wf")
	if def.Revision != 1 || def.Name != "v1" {
		t.Fatalf("proposal altered canonical before acceptance: %+v", def)
	}

	// An equivalent second proposal dedups and bumps support.
	_, out2, err := h(context.Background(), nil, ProposeProjectLearningInput{
		ArtifactType: "workflow", TargetId: "wf", Candidate: candidate, SourceRunIds: []string{"run-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out2.Deduplicated || out2.SupportCount != 2 {
		t.Fatalf("second proposal should dedup with support 2: %+v", out2)
	}

	// Acceptance (via the same adapter the HTTP accept path uses) applies the
	// candidate and never enables the workflow.
	reviews := &artifact.ReviewStore{WorkspaceRoot: a.Workspace.Root}
	content, _, err := reviews.GetProposal(out.ReviewId, 1)
	if err != nil {
		t.Fatalf("read proposal content: %v", err)
	}
	adapter := &learning.WorkflowAdapter{Root: a.Workspace.Root}
	if _, err := adapter.CreateVersion("wf", a.Workspace.ID, content, time.Now()); err != nil {
		t.Fatalf("accept-apply: %v", err)
	}
	applied, _ := store.Get("wf")
	if applied.Revision != 2 || applied.Name != "v2 (improved)" {
		t.Fatalf("acceptance did not apply candidate: %+v", applied)
	}
	if applied.Enabled {
		t.Fatal("acceptance enabled the workflow; activation must stay separate")
	}
}
