package handoff

import (
	"strings"
	"testing"
)

// sampleContributions returns one contribution per role that together assemble
// to a fully-populated capsule equivalent to sampleCapsule, so builder-based
// tests exercise the same validation branches as the hand-built ones.
func sampleContributions() (SemarContribution, GarengContribution, PetrukContribution, BagongContribution) {
	semar := SemarContribution{
		Objective: Objective{
			Statement:  "Add retry handling for affiliate payout processing.",
			SourceRefs: []string{"jira:TRF-1842"},
		},
		CurrentPhase:       "implementation",
		AcceptedPlan:       &PlanRef{ID: "payout-retry-plan", Version: 4},
		RoleConfigRevision: 7,
	}
	gareng := GarengContribution{
		OpenContradictions: []string{"contradiction-retry-metric-tag"},
		UnresolvedRisks:    []string{"Production metric cardinality is not yet measured."},
		ImpactSummary: &ImpactSummary{
			RequiredRepositories: []string{"affiliate-api", "affiliate-e2e"},
			ExcludedRepositories: []string{"affiliate-ui"},
		},
	}
	petruk := PetrukContribution{
		ChangedRepositories: []string{"affiliate-api", "affiliate-e2e"},
		CompletedTasks:      []string{"task-71", "task-72"},
		CurrentTask:         &TaskRef{ID: "task-73", NextAction: "Add retry exhaustion integration test."},
	}
	bagong := BagongContribution{
		Evidence: []string{"evidence-unit-tests-14"},
		Dossier:  &DossierRef{ID: "change-affiliate-retry-017", Status: "implementing"},
	}
	return semar, gareng, petruk, bagong
}

// TestAssembleCreateGetRoundTrip assembles a capsule from all four
// contributions, persists it, reads it back, and asserts every role's section
// survived the round trip (HANDOFF-007..010).
func TestAssembleCreateGetRoundTrip(t *testing.T) {
	root := t.TempDir()
	semar, gareng, petruk, bagong := sampleContributions()

	capsule := Assemble("affiliate-platform", "run-1842", semar, &gareng, &petruk, &bagong)
	// The assembled capsule must already be valid and Create-ready.
	if capsule.Version != SupportedVersion {
		t.Fatalf("Assemble must stamp schema version, got %q", capsule.Version)
	}
	if capsule.Id != "handoff-run-1842" {
		t.Fatalf("Assemble derived id = %q, want handoff-run-1842", capsule.Id)
	}
	if capsule.ProjectId != "affiliate-platform" || capsule.RunId != "run-1842" {
		t.Fatalf("Assemble project/run mismatch: %q / %q", capsule.ProjectId, capsule.RunId)
	}

	if _, err := Create(root, capsule); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := Get(root, "handoff-run-1842")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Semar's section.
	if got.Objective.Statement != semar.Objective.Statement {
		t.Errorf("Semar objective lost: %q", got.Objective.Statement)
	}
	if len(got.Objective.SourceRefs) != 1 || got.Objective.SourceRefs[0] != "jira:TRF-1842" {
		t.Errorf("Semar source_refs lost: %v", got.Objective.SourceRefs)
	}
	if got.CurrentPhase != "implementation" {
		t.Errorf("Semar current_phase lost: %q", got.CurrentPhase)
	}
	if got.AcceptedPlan == nil || got.AcceptedPlan.Id == nil || *got.AcceptedPlan.Id != "payout-retry-plan" ||
		got.AcceptedPlan.Version == nil || *got.AcceptedPlan.Version != 4 {
		t.Errorf("Semar accepted_plan lost: %+v", got.AcceptedPlan)
	}
	if got.RoleConfigurationRevision == nil || *got.RoleConfigurationRevision != 7 {
		t.Errorf("Semar role_configuration_revision lost: %+v", got.RoleConfigurationRevision)
	}
	if got.CreatedBy == nil || got.CreatedBy.Role == nil || *got.CreatedBy.Role != "semar" {
		t.Errorf("created_by.role should record semar: %+v", got.CreatedBy)
	}

	// Gareng's section.
	if len(got.OpenContradictions) != 1 || got.OpenContradictions[0] != "contradiction-retry-metric-tag" {
		t.Errorf("Gareng open_contradictions lost: %v", got.OpenContradictions)
	}
	if len(got.UnresolvedRisks) != 1 {
		t.Errorf("Gareng unresolved_risks lost: %v", got.UnresolvedRisks)
	}
	if got.ImpactSummary == nil || len(got.ImpactSummary.RequiredRepositories) != 2 ||
		len(got.ImpactSummary.ExcludedRepositories) != 1 {
		t.Errorf("Gareng impact_summary lost: %+v", got.ImpactSummary)
	}

	// Petruk's section.
	if len(got.ChangedRepositories) != 2 {
		t.Errorf("Petruk changed_repositories lost: %v", got.ChangedRepositories)
	}
	if len(got.CompletedTasks) != 2 {
		t.Errorf("Petruk completed_tasks lost: %v", got.CompletedTasks)
	}
	if got.CurrentTask == nil || got.CurrentTask.Id == nil || *got.CurrentTask.Id != "task-73" ||
		got.CurrentTask.NextAction == nil || *got.CurrentTask.NextAction != "Add retry exhaustion integration test." {
		t.Errorf("Petruk current_task lost: %+v", got.CurrentTask)
	}

	// Bagong's section.
	if len(got.Evidence) != 1 || got.Evidence[0] != "evidence-unit-tests-14" {
		t.Errorf("Bagong evidence lost: %v", got.Evidence)
	}
	if got.Dossier == nil || got.Dossier.Id == nil || *got.Dossier.Id != "change-affiliate-retry-017" ||
		got.Dossier.Status == nil || *got.Dossier.Status != "implementing" {
		t.Errorf("Bagong dossier lost: %+v", got.Dossier)
	}
}

// TestBuilderFluentEquivalentToAssemble asserts the fluent NewCapsule/With*
// path and the one-shot Assemble path produce byte-identical capsules.
func TestBuilderFluentEquivalentToAssemble(t *testing.T) {
	semar, gareng, petruk, bagong := sampleContributions()

	fluent := NewCapsule("affiliate-platform", "run-1842", semar).
		WithGareng(gareng).
		WithPetruk(petruk).
		WithBagong(bagong).
		Build()
	oneShot := Assemble("affiliate-platform", "run-1842", semar, &gareng, &petruk, &bagong)

	root := t.TempDir()
	if _, err := Create(root, fluent); err != nil {
		t.Fatalf("Create fluent: %v", err)
	}
	// Both must carry the same objective/phase/section pointers where set.
	if fluent.Objective.Statement != oneShot.Objective.Statement ||
		len(fluent.Objective.SourceRefs) != len(oneShot.Objective.SourceRefs) {
		t.Errorf("objective differs: %+v vs %+v", fluent.Objective, oneShot.Objective)
	}
	if fluent.CurrentPhase != oneShot.CurrentPhase {
		t.Errorf("current_phase differs: %q vs %q", fluent.CurrentPhase, oneShot.CurrentPhase)
	}
	if (fluent.ImpactSummary == nil) != (oneShot.ImpactSummary == nil) ||
		(fluent.CurrentTask == nil) != (oneShot.CurrentTask == nil) ||
		(fluent.Dossier == nil) != (oneShot.Dossier == nil) {
		t.Errorf("section presence differs between fluent and one-shot builders")
	}
}

// TestAssembleValidatesResumableWithNilDeps confirms an assembled capsule is
// shaped correctly: with no lookups wired (all deps nil) every precondition is
// skipped and the verdict is resumable, proving the builder produced a
// well-formed capsule rather than one that trips validation on its own shape.
func TestAssembleValidatesResumableWithNilDeps(t *testing.T) {
	root := t.TempDir()
	semar, gareng, petruk, bagong := sampleContributions()
	capsule := Assemble("affiliate-platform", "run-1842", semar, &gareng, &petruk, &bagong)
	if _, err := Create(root, capsule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	res, err := Validate(root, capsule.Id, ValidationDeps{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusResumable {
		t.Fatalf("nil-deps validate of assembled capsule = %s, want resumable", res.Status)
	}
}

// TestAssembleSemarOnlyIsValid confirms a capsule built from only the required
// Semar contribution is still a valid, resumable capsule (the optional role
// sections are genuinely optional).
func TestAssembleSemarOnlyIsValid(t *testing.T) {
	root := t.TempDir()
	semar, _, _, _ := sampleContributions()
	capsule := Assemble("affiliate-platform", "run-1842", semar, nil, nil, nil)
	if _, err := Create(root, capsule); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if capsule.OpenContradictions != nil || capsule.CurrentTask != nil || capsule.Dossier != nil {
		t.Fatalf("Semar-only capsule must leave other sections unset: %+v", capsule)
	}
	res, err := Validate(root, capsule.Id, ValidationDeps{})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusResumable {
		t.Fatalf("Semar-only assembled capsule = %s, want resumable", res.Status)
	}
}

// TestAssembledSupersedeThenResumeRejected covers the §43 acceptance that a
// superseded capsule must never resume silently: assemble, persist, supersede,
// then validate must reject with the superseded status.
func TestAssembledSupersedeThenResumeRejected(t *testing.T) {
	root := t.TempDir()
	semar, gareng, petruk, bagong := sampleContributions()
	capsule := Assemble("affiliate-platform", "run-1842", semar, &gareng, &petruk, &bagong)
	if _, err := Create(root, capsule); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := Supersede(root, capsule.Id); err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	res, err := Validate(root, capsule.Id, greenDeps())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusSuperseded {
		t.Fatalf("superseded assembled capsule resumed as %s, want superseded", res.Status)
	}
}

// --- HANDOFF-013 cross-agent resume / HANDOFF-014 stale reference (§43) ---
//
// The following tests strengthen resume validation against the specific stale
// references §43 enumerates, each built through the assembly API so the
// cross-agent path (assemble on one client, resume on another) is exercised
// end to end: a changed plan version blocks, a superseded dossier is rejected,
// and a materially changed contradiction forces a refresh with concrete
// required_refresh steps.

func TestCrossAgentResumeBlockedOnChangedPlanVersion(t *testing.T) {
	root := t.TempDir()
	semar, gareng, petruk, bagong := sampleContributions()
	capsule := Assemble("affiliate-platform", "run-1842", semar, &gareng, &petruk, &bagong)
	if _, err := Create(root, capsule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The resuming agent finds the pinned plan version is gone (plan advanced).
	deps := greenDeps()
	deps.PlanVersionExists = func(id string, version int) (bool, error) {
		if id != "payout-retry-plan" || version != 4 {
			t.Errorf("unexpected plan lookup: %q v%d", id, version)
		}
		return false, nil
	}
	res, err := Validate(root, capsule.Id, deps)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusBlocked {
		t.Fatalf("changed plan version = %s, want blocked", res.Status)
	}
	if !strings.Contains(strings.Join(res.ChangesSinceHandoff, " | "), "payout-retry-plan") {
		t.Fatalf("blocked reason must name the missing plan version: %v", res.ChangesSinceHandoff)
	}
}

func TestCrossAgentResumeRejectedOnSupersededDossier(t *testing.T) {
	root := t.TempDir()
	semar, gareng, petruk, bagong := sampleContributions()
	capsule := Assemble("affiliate-platform", "run-1842", semar, &gareng, &petruk, &bagong)
	if _, err := Create(root, capsule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	deps := greenDeps()
	var checked string
	deps.DossierSuperseded = func(id string) (bool, error) {
		checked = id
		return true, nil
	}
	res, err := Validate(root, capsule.Id, deps)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusSuperseded {
		t.Fatalf("superseded dossier = %s, want superseded", res.Status)
	}
	if checked != "change-affiliate-retry-017" {
		t.Fatalf("Validate checked dossier %q, want the assembled dossier id", checked)
	}
}

func TestCrossAgentResumeRefreshOnChangedContradiction(t *testing.T) {
	root := t.TempDir()
	semar, gareng, petruk, bagong := sampleContributions()
	capsule := Assemble("affiliate-platform", "run-1842", semar, &gareng, &petruk, &bagong)
	if _, err := Create(root, capsule); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Only the contradiction changed; everything else still holds. This must be
	// recoverable (refresh_required), not a hard block.
	deps := greenDeps()
	deps.ContradictionsChanged = func(ids []string) ([]string, error) { return ids, nil }
	res, err := Validate(root, capsule.Id, deps)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Status != StatusRefreshRequired {
		t.Fatalf("changed contradiction = %s, want refresh_required", res.Status)
	}
	if !strings.Contains(strings.Join(res.ChangesSinceHandoff, " | "), "contradiction-retry-metric-tag") {
		t.Fatalf("changes must name the changed contradiction: %v", res.ChangesSinceHandoff)
	}
	if !strings.Contains(strings.Join(res.RequiredRefresh, " | "), "refresh contradiction summary") {
		t.Fatalf("required_refresh must instruct a contradiction refresh: %v", res.RequiredRefresh)
	}
}
