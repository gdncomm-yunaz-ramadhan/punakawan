package capsule

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newTestStore mirrors internal/taskcontext's own helper, skipping if dolt
// is not installed.
func newTestStore(t *testing.T) *knowledge.Store {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
	dir := t.TempDir()
	sup := tools.New(dir)
	store, err := knowledge.Open(sup, filepath.Join(dir, "knowledge"))
	if err != nil {
		t.Fatalf("knowledge.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return store
}

func baseRecord(id string, typ protocol.KnowledgeRecordType, title string) protocol.KnowledgeRecord {
	return protocol.KnowledgeRecord{
		Id:     id,
		Type:   typ,
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
}

func TestBuildAssemblesCapsuleAndResolvesReferences(t *testing.T) {
	store := newTestStore(t)
	req := baseRecord("pkw:req/smoke/REQ-1", protocol.KnowledgeRecordTypeRequirement, "Refund settles same day")
	if err := store.Put(req); err != nil {
		t.Fatalf("seed requirement: %v", err)
	}

	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	c, err := Build(store, "cap-1", now, BuildInput{
		TaskID:         "bd-task-1",
		Role:           protocol.ContextCapsuleRolePetruk,
		Objective:      "Implement the refund flow",
		RequirementIDs: []string{req.Id},
		AllowedTools:   []string{"write_file"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if c.Id != "cap-1" || c.TaskId != "bd-task-1" || c.Role != protocol.ContextCapsuleRolePetruk {
		t.Fatalf("Build = %+v, want id/task_id/role set", c)
	}
	if len(c.Requirements) != 1 || c.Requirements[0].Id != req.Id {
		t.Fatalf("Requirements = %+v, want %q resolved", c.Requirements, req.Id)
	}
	if c.Requirements[0].Summary == nil || *c.Requirements[0].Summary != req.Title {
		t.Fatalf("Requirements[0].Summary = %v, want %q", c.Requirements[0].Summary, req.Title)
	}
	if c.Digest == "" {
		t.Fatal("Digest is empty, want it computed")
	}
}

func TestBuildProjectMetadataIsCarriedButNotDigested(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)

	base := BuildInput{
		TaskID:    "bd-task-1",
		Role:      protocol.ContextCapsuleRolePetruk,
		Objective: "Implement the refund flow",
	}
	withMeta := base
	withMeta.ProjectMetadata = []protocol.ContextCapsuleProjectMetadataElem{
		{Key: "jira.project_key", Value: "TRF", Rendered: "jira.project_key\n  Value: TRF"},
	}

	plain, err := Build(store, "cap-1", now, base)
	if err != nil {
		t.Fatalf("Build plain: %v", err)
	}
	meta, err := Build(store, "cap-1", now, withMeta)
	if err != nil {
		t.Fatalf("Build with metadata: %v", err)
	}

	if len(meta.ProjectMetadata) != 1 || meta.ProjectMetadata[0].Key != "jira.project_key" {
		t.Fatalf("ProjectMetadata = %+v, want the injected entry carried onto the capsule", meta.ProjectMetadata)
	}
	if len(plain.ProjectMetadata) != 0 {
		t.Fatalf("ProjectMetadata = %+v, want none when the caller injected none", plain.ProjectMetadata)
	}
	// §6.3: project_metadata is derived context, not a digest input. Adding it
	// must not change the capsule's identity.
	if plain.Digest != meta.Digest {
		t.Fatalf("digest changed with project_metadata: %q vs %q; it must be excluded from the §6.3 payload", plain.Digest, meta.Digest)
	}
}

func TestBuildRejectsForbiddenKnowledgeType(t *testing.T) {
	store := newTestStore(t)
	plan := baseRecord("pkw:petrukplan/smoke/PLAN-1", protocol.KnowledgeRecordTypePetrukPlan, "Petruk's plan")
	if err := store.Put(plan); err != nil {
		t.Fatalf("seed petruk-plan: %v", err)
	}

	_, err := Build(store, "cap-1", time.Now().UTC(), BuildInput{
		TaskID:       "bd-task-1",
		Role:         protocol.ContextCapsuleRoleBagong,
		Objective:    "Verify the refund flow",
		KnowledgeIDs: []string{plan.Id},
	})
	if err == nil {
		t.Fatal("expected an error citing a petruk-plan record in a bagong capsule")
	}
}

func TestBuildRejectsForbiddenTool(t *testing.T) {
	store := newTestStore(t)
	_, err := Build(store, "cap-1", time.Now().UTC(), BuildInput{
		TaskID:       "bd-task-1",
		Role:         protocol.ContextCapsuleRoleBagong,
		Objective:    "Verify the refund flow",
		AllowedTools: []string{"write_file"},
	})
	if err == nil {
		t.Fatal("expected an error granting write_file to a bagong capsule")
	}
}

func TestBuildRequiresTaskIDAndObjective(t *testing.T) {
	store := newTestStore(t)
	if _, err := Build(store, "cap-1", time.Now().UTC(), BuildInput{Role: protocol.ContextCapsuleRolePetruk, Objective: "x"}); err == nil {
		t.Fatal("expected an error for a missing task id")
	}
	if _, err := Build(store, "cap-1", time.Now().UTC(), BuildInput{TaskID: "bd-task-1", Role: protocol.ContextCapsuleRolePetruk}); err == nil {
		t.Fatal("expected an error for a missing objective")
	}
}

func TestBuildNormalizesNilRequiredArraysToEmpty(t *testing.T) {
	store := newTestStore(t)
	c, err := Build(store, "cap-1", time.Now().UTC(), BuildInput{
		TaskID:    "bd-task-1",
		Role:      protocol.ContextCapsuleRolePetruk,
		Objective: "Implement the refund flow",
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if c.AllowedTools == nil || c.ForbiddenActions == nil {
		t.Fatalf("AllowedTools/ForbiddenActions = %v/%v, want non-nil empty slices (schema requires a non-null array)", c.AllowedTools, c.ForbiddenActions)
	}
}
