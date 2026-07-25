package handoff

import (
	"strings"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func ptr[T any](v T) *T { return &v }

// sampleCapsule builds a fully-populated capsule so validation has every
// precondition to check and ResumeContext has every field to project.
func sampleCapsule(id string) protocol.HandoffCapsule {
	return protocol.HandoffCapsule{
		Id:        id,
		ProjectId: "affiliate-platform",
		RunId:     "run-1842",
		Objective: protocol.HandoffCapsuleObjective{
			Statement:  "Add retry handling for affiliate payout processing.",
			SourceRefs: []string{"jira:TRF-1842"},
		},
		CurrentPhase:              "implementation",
		AcceptedPlan:              &protocol.HandoffCapsuleAcceptedPlan{Id: ptr("payout-retry-plan"), Version: ptr(4)},
		RoleConfigurationRevision: ptr(7),
		CompletedTasks:            []string{"task-71", "task-72"},
		CurrentTask:               &protocol.HandoffCapsuleCurrentTask{Id: ptr("task-73"), NextAction: ptr("Add retry exhaustion integration test.")},
		ChangedRepositories:       []string{"affiliate-api", "affiliate-e2e"},
		OpenContradictions:        []string{"contradiction-retry-metric-tag"},
		UnresolvedRisks:           []string{"Production metric cardinality is not yet measured."},
		Evidence:                  []string{"evidence-unit-tests-14"},
		Dossier:                   &protocol.HandoffCapsuleDossier{Id: ptr("change-affiliate-retry-017"), Status: ptr("implementing")},
	}
}

// greenDeps returns deps under which every precondition passes: a resumable
// capsule. Individual tests override single fields to exercise each branch.
func greenDeps() ValidationDeps {
	return ValidationDeps{
		PlanVersionExists:      func(string, int) (bool, error) { return true, nil },
		RoleConfigRevision:     func() (int, error) { return 7, nil },
		TaskIsCurrent:          func(string) (bool, error) { return true, nil },
		ContradictionsChanged:  func([]string) ([]string, error) { return nil, nil },
		RepositoryStateMatches: func([]string) (bool, error) { return true, nil },
		EvidenceExists:         func([]string) ([]string, error) { return nil, nil },
		DossierSuperseded:      func(string) (bool, error) { return false, nil },
	}
}

func TestCreateGetListRoundTrip(t *testing.T) {
	root := t.TempDir()

	if ids, err := List(root); err != nil || len(ids) != 0 {
		t.Fatalf("List on empty workspace = %v, %v; want [], nil", ids, err)
	}

	// Get on a missing capsule synthesizes an empty one, never errors.
	miss, err := Get(root, "nope")
	if err != nil {
		t.Fatalf("Get missing: %v", err)
	}
	if miss.Id != "nope" || miss.Version != SupportedVersion {
		t.Fatalf("Get missing synth = %+v", miss)
	}

	created, err := Create(root, sampleCapsule("h1"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Version != SupportedVersion || created.CreatedAt == nil {
		t.Fatalf("Create must stamp version and CreatedAt: %+v", created)
	}

	got, err := Get(root, "h1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RunId != "run-1842" || got.CurrentPhase != "implementation" {
		t.Fatalf("Get round-trip mismatch: %+v", got)
	}

	if _, err := Create(root, sampleCapsule("h2")); err != nil {
		t.Fatalf("Create h2: %v", err)
	}
	ids, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 2 || ids[0] != "h1" || ids[1] != "h2" {
		t.Fatalf("List = %v, want sorted [h1 h2]", ids)
	}
}

func TestSupersedeThenValidateSuperseded(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleCapsule("h1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := Supersede(root, "h1"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	got, _ := Get(root, "h1")
	if got.Superseded == nil || !*got.Superseded {
		t.Fatalf("Supersede did not set flag: %+v", got.Superseded)
	}

	// A superseded capsule validates to superseded even with all-green deps.
	res, err := Validate(root, "h1", greenDeps())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusSuperseded {
		t.Fatalf("Validate superseded capsule = %s, want superseded", res.Status)
	}
}

func TestValidateResumableWhenAllGreen(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleCapsule("h1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	res, err := Validate(root, "h1", greenDeps())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusResumable {
		t.Fatalf("Validate all-green = %s, want resumable", res.Status)
	}
	if len(res.ChangesSinceHandoff) != 0 || len(res.RequiredRefresh) != 0 {
		t.Fatalf("resumable must carry no changes/refresh, got %+v", res)
	}
}

func TestValidateDossierSuperseded(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleCapsule("h1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	deps := greenDeps()
	deps.DossierSuperseded = func(string) (bool, error) { return true, nil }
	res, err := Validate(root, "h1", deps)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusSuperseded {
		t.Fatalf("dossier superseded = %s, want superseded", res.Status)
	}
}

func TestValidateBlockedOnMissingPlanVersion(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleCapsule("h1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	deps := greenDeps()
	deps.PlanVersionExists = func(string, int) (bool, error) { return false, nil }
	res, err := Validate(root, "h1", deps)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusBlocked {
		t.Fatalf("missing plan version = %s, want blocked", res.Status)
	}
	if len(res.ChangesSinceHandoff) == 0 {
		t.Fatal("blocked must explain what is missing")
	}
}

func TestValidateBlockedOnMissingEvidence(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleCapsule("h1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	deps := greenDeps()
	deps.EvidenceExists = func(ids []string) ([]string, error) { return ids, nil } // all missing
	res, err := Validate(root, "h1", deps)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusBlocked {
		t.Fatalf("missing evidence = %s, want blocked", res.Status)
	}
}

func TestValidateInvalidOnRepoMismatch(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleCapsule("h1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	deps := greenDeps()
	deps.RepositoryStateMatches = func([]string) (bool, error) { return false, nil }
	res, err := Validate(root, "h1", deps)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusInvalid {
		t.Fatalf("repo mismatch = %s, want invalid", res.Status)
	}
}

func TestValidateRefreshRequiredOnRoleAndContradictionChange(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleCapsule("h1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	deps := greenDeps()
	deps.RoleConfigRevision = func() (int, error) { return 8, nil } // capsule recorded 7
	deps.ContradictionsChanged = func(ids []string) ([]string, error) { return ids, nil }

	res, err := Validate(root, "h1", deps)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusRefreshRequired {
		t.Fatalf("role+contradiction change = %s, want refresh_required", res.Status)
	}

	joinedChanges := strings.Join(res.ChangesSinceHandoff, " | ")
	if !strings.Contains(joinedChanges, "revision 7 to 8") {
		t.Fatalf("ChangesSinceHandoff missing role revision change: %v", res.ChangesSinceHandoff)
	}
	if !strings.Contains(joinedChanges, "contradiction-retry-metric-tag") {
		t.Fatalf("ChangesSinceHandoff missing contradiction change: %v", res.ChangesSinceHandoff)
	}
	joinedRefresh := strings.Join(res.RequiredRefresh, " | ")
	if !strings.Contains(joinedRefresh, "reload role configuration") ||
		!strings.Contains(joinedRefresh, "refresh contradiction summary") {
		t.Fatalf("RequiredRefresh missing steps: %v", res.RequiredRefresh)
	}
}

func TestResumeContextIsCompact(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleCapsule("h1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	ctx, err := ResumeContext(root, "h1")
	if err != nil {
		t.Fatalf("ResumeContext: %v", err)
	}

	// The compact fields must be present.
	for _, k := range []string{"objective", "current_phase", "current_task", "accepted_plan", "open_contradictions", "unresolved_risks"} {
		if _, ok := ctx[k]; !ok {
			t.Fatalf("ResumeContext missing compact field %q; got keys %v", k, keysOf(ctx))
		}
	}
	// The bulky fields must be omitted - a resumer fetches them on demand.
	for _, k := range []string{"completed_tasks", "changed_repositories", "evidence", "impact_summary"} {
		if _, ok := ctx[k]; ok {
			t.Fatalf("ResumeContext should omit bulky field %q", k)
		}
	}

	task, ok := ctx["current_task"].(map[string]any)
	if !ok || task["id"] != "task-73" || task["next_action"] != "Add retry exhaustion integration test." {
		t.Fatalf("current_task not projected as references: %v", ctx["current_task"])
	}
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
