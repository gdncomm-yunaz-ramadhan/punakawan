package mcpserver

import (
	"context"
	"strings"
	"testing"
)

// TestProposeProjectLearningKnowledgeNotFoundGuidesToCreatePath guards
// against a regression. propose_project_learning is an
// improve-an-existing-artifact tool: for the knowledge pillar it needs an
// existing record id and cannot mint one from scratch (correct by design). The
// reported confusion was that this looked like a dead end. The not-found error
// must now point the caller at the direct create path instead of just failing
// opaquely or forcing an unrelated dossier/run workflow.
func TestProposeProjectLearningKnowledgeNotFoundGuidesToCreatePath(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)

	h := proposeProjectLearningHandler(a)
	_, _, err := h(context.Background(), nil, ProposeProjectLearningInput{
		ArtifactType: "knowledge",
		TargetId:     "does-not-exist",
		Candidate:    map[string]any{"type": "insight", "body": "hello"},
		Rationale:    "learn",
	})
	if err == nil {
		t.Fatal("expected an error proposing against a non-existent knowledge record")
	}
	msg := err.Error()
	if !strings.Contains(msg, "not found") {
		t.Fatalf("error should say the target was not found, got: %q", msg)
	}
	if !strings.Contains(msg, "create_knowledge_record") {
		t.Fatalf("error should point to create_knowledge_record, got: %q", msg)
	}
}

// TestCreateKnowledgeRecordNeedsNoTrackedRun is the end-to-end regression
// proving an agent can persist reusable knowledge without inventing a run
// id, creating a capsule, or pretending the knowledge is a context dossier.
func TestCreateKnowledgeRecordNeedsNoTrackedRun(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var out map[string]any
	callTool(t, cs, "create_knowledge_record", map[string]any{
		"type":    "decision",
		"title":   "Use bounded retries",
		"content": "Retry transient reads at most twice.",
		"tags":    []string{"reliability"},
	}, &out)

	id, _ := out["id"].(string)
	if id == "" {
		t.Fatalf("create_knowledge_record did not return a knowledge id: %+v", out)
	}
	store, err := a.OpenKnowledge()
	if err != nil {
		t.Fatalf("OpenKnowledge: %v", err)
	}
	rec, err := store.Get(id)
	if err != nil {
		t.Fatalf("newly created knowledge record %q not retrievable: %v", id, err)
	}
	if rec.Type != "decision" || rec.Content == nil || *rec.Content != "Retry transient reads at most twice." {
		t.Fatalf("stored record = %+v", rec)
	}
	if rec.Validity.State != "inferred" || rec.Extraction.Method != "model-assisted" {
		t.Fatalf("default provenance = %+v/%+v, want inferred/model-assisted", rec.Validity, rec.Extraction)
	}
}

func TestCreateKnowledgeRecordRejectsStructuredRoleOutput(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	_, err := createKnowledgeRecord(a, CreateKnowledgeRecordInput{
		Type:    "bagong-review",
		Title:   "Bypass review",
		Content: "This must not bypass the senior review schema.",
	})
	if err == nil || !strings.Contains(err.Error(), "submit_lane_bagong_review") {
		t.Fatalf("expected dedicated-tool guidance, got %v", err)
	}
}

func TestCreateKnowledgeRecordRejectsExistingId(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	id := "pkw:knowledge/" + a.Workspace.ID + "/stable"
	in := CreateKnowledgeRecordInput{Id: id, Type: "decision", Title: "First", Content: "First value"}
	if _, err := createKnowledgeRecord(a, in); err != nil {
		t.Fatalf("first create: %v", err)
	}
	in.Content = "Overwrite"
	if _, err := createKnowledgeRecord(a, in); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second create should reject overwrite, got %v", err)
	}
}
