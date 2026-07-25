package dossier

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// sampleDossier builds a minimally-populated dossier for round-trip tests. It
// deliberately fills the fields the summary indicators and conformance logic
// read so those computations have something to report.
func sampleDossier(id string) protocol.ChangeDossier {
	return protocol.ChangeDossier{
		Id:        id,
		ProjectId: "proj-1",
		Title:     "Add retry handling",
		Status:    protocol.ChangeDossierStatusDraft,
		Objective: protocol.ChangeDossierObjective{
			Statement:  "Add bounded retry handling for transient failures.",
			SourceRefs: []string{"jira:TRF-1842"},
		},
		Requirements: &protocol.ChangeDossierRequirements{
			Covered:   []string{"req-a", "req-b"},
			Uncovered: []string{"req-c"},
		},
		Impact: &protocol.ChangeDossierImpact{
			Repositories: []string{"repo-a", "repo-b"},
		},
		PlanConformance: &protocol.ChangeDossierPlanConformance{
			Implemented: ptr(10),
			Partial:     ptr(1),
			Missing:     ptr(0),
		},
	}
}

func TestCreateGetListRoundTrip(t *testing.T) {
	root := t.TempDir()

	if ids, err := List(root); err != nil || len(ids) != 0 {
		t.Fatalf("List on empty workspace = %v, %v; want [], nil", ids, err)
	}

	// Get on a missing dossier synthesizes an empty draft, never errors.
	got, err := Get(root, "missing")
	if err != nil {
		t.Fatalf("Get missing: %v", err)
	}
	if got.Dossier.Status != protocol.ChangeDossierStatusDraft || got.Dossier.Id != "missing" {
		t.Fatalf("Get missing synth = %+v, want draft id=missing", got.Dossier)
	}
	if got.Claims == nil || got.Evidence == nil {
		t.Fatalf("Get missing: claims/evidence must be non-nil, got %v / %v", got.Claims, got.Evidence)
	}

	created, err := Create(root, sampleDossier("d1"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Version != SupportedVersion {
		t.Fatalf("Create version = %q, want %q", created.Version, SupportedVersion)
	}
	if created.CreatedAt == nil || created.UpdatedAt == nil {
		t.Fatal("Create must default CreatedAt/UpdatedAt")
	}

	loaded, err := Get(root, "d1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loaded.Dossier.Title != "Add retry handling" || loaded.Dossier.ProjectId != "proj-1" {
		t.Fatalf("Get round-trip mismatch: %+v", loaded.Dossier)
	}

	if _, err := Create(root, sampleDossier("d2")); err != nil {
		t.Fatalf("Create d2: %v", err)
	}
	ids, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 2 || ids[0] != "d1" || ids[1] != "d2" {
		t.Fatalf("List = %v, want sorted [d1 d2]", ids)
	}
}

func TestPutSnapshotsPriorVersion(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleDossier("d1")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	d, _ := Get(root, "d1")
	d.Dossier.Status = protocol.ChangeDossierStatusContextReady
	if _, err := Put(root, d.Dossier); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// The pre-Put current.yaml (draft) should now be snapshotted as version 1.
	if _, err := readCurrent(root, "d1"); err != nil {
		t.Fatalf("readCurrent: %v", err)
	}
	if got := nextVersion(root, "d1"); got != 2 {
		t.Fatalf("nextVersion after one Put = %d, want 2 (one snapshot exists)", got)
	}
}

func TestLifecycleLegalAndIllegalAdvance(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleDossier("d1")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Legal linear progression.
	legal := []protocol.ChangeDossierStatus{
		protocol.ChangeDossierStatusContextReady,
		protocol.ChangeDossierStatusPlanned,
		protocol.ChangeDossierStatusImplementing,
		protocol.ChangeDossierStatusAwaitingVerification,
		protocol.ChangeDossierStatusVerified,
		protocol.ChangeDossierStatusCompleted,
	}
	for _, to := range legal {
		if err := Advance(root, "d1", to); err != nil {
			t.Fatalf("Advance -> %s: %v", to, err)
		}
	}

	// Illegal: skipping a step (draft -> planned).
	if _, err := Create(root, sampleDossier("d2")); err != nil {
		t.Fatalf("Create d2: %v", err)
	}
	if err := Advance(root, "d2", protocol.ChangeDossierStatusPlanned); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("skip-step advance = %v, want ErrIllegalTransition", err)
	}

	// Universal escapes are legal from any state.
	if err := Advance(root, "d2", protocol.ChangeDossierStatusDisputed); err != nil {
		t.Fatalf("any -> disputed: %v", err)
	}
	if _, err := Create(root, sampleDossier("d3")); err != nil {
		t.Fatalf("Create d3: %v", err)
	}
	if err := Advance(root, "d3", protocol.ChangeDossierStatusSuperseded); err != nil {
		t.Fatalf("any -> superseded: %v", err)
	}
}

func TestFinalizeBlockedThenClean(t *testing.T) {
	root := t.TempDir()

	// A verified dossier that is otherwise clean finalizes.
	clean := sampleDossier("clean")
	clean.Status = protocol.ChangeDossierStatusVerified
	if _, err := Create(root, clean); err != nil {
		t.Fatalf("Create clean: %v", err)
	}
	if err := Finalize(root, "clean"); err != nil {
		t.Fatalf("Finalize clean: %v", err)
	}
	if got, _ := Get(root, "clean"); got.Dossier.Status != protocol.ChangeDossierStatusCompleted {
		t.Fatalf("clean status = %s, want completed", got.Dossier.Status)
	}

	// Unresolved contradiction blocks.
	contra := sampleDossier("contra")
	contra.Status = protocol.ChangeDossierStatusVerified
	contra.Contradictions = &protocol.ChangeDossierContradictions{Unresolved: []string{"c-1"}}
	if _, err := Create(root, contra); err != nil {
		t.Fatalf("Create contra: %v", err)
	}
	if err := Finalize(root, "contra"); !errors.Is(err, ErrBlockingFindings) {
		t.Fatalf("Finalize contra = %v, want ErrBlockingFindings", err)
	}

	// Missing plan item blocks.
	miss := sampleDossier("miss")
	miss.Status = protocol.ChangeDossierStatusVerified
	miss.PlanConformance = &protocol.ChangeDossierPlanConformance{Missing: ptr(2)}
	if _, err := Create(root, miss); err != nil {
		t.Fatalf("Create miss: %v", err)
	}
	err := Finalize(root, "miss")
	if !errors.Is(err, ErrBlockingFindings) {
		t.Fatalf("Finalize miss = %v, want ErrBlockingFindings", err)
	}
	var be *BlockingError
	if !errors.As(err, &be) || len(be.Blockers) == 0 {
		t.Fatalf("Finalize miss: want *BlockingError with blockers, got %v", err)
	}

	// A disputed claim blocks even when the dossier itself is clean.
	dc := sampleDossier("dc")
	dc.Status = protocol.ChangeDossierStatusVerified
	if _, err := Create(root, dc); err != nil {
		t.Fatalf("Create dc: %v", err)
	}
	if _, err := AddClaim(root, "dc", protocol.DossierClaim{
		Id:        "claim-1",
		Type:      "implementation",
		Statement: "API is compatible.",
		Producer:  protocol.DossierClaimProducer{Role: protocol.DossierClaimProducerRolePetruk},
	}); err != nil {
		t.Fatalf("AddClaim: %v", err)
	}
	if _, err := DisputeClaim(root, "dc", "claim-1", "bagong", "counterexample found"); err != nil {
		t.Fatalf("DisputeClaim: %v", err)
	}
	if err := Finalize(root, "dc"); !errors.Is(err, ErrBlockingFindings) {
		t.Fatalf("Finalize dc = %v, want ErrBlockingFindings (disputed claim)", err)
	}
}

func TestClaimsVerifyDisputeAndSelfVerification(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleDossier("d1")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	claim := protocol.DossierClaim{
		Id:        "claim-api",
		Type:      "compatibility",
		Statement: "Public API remains backward compatible.",
		Producer:  protocol.DossierClaimProducer{Role: protocol.DossierClaimProducerRolePetruk},
	}
	added, err := AddClaim(root, "d1", claim)
	if err != nil {
		t.Fatalf("AddClaim: %v", err)
	}
	if added.Status != protocol.DossierClaimStatusClaimed {
		t.Fatalf("AddClaim default status = %s, want claimed", added.Status)
	}
	if added.DossierId == nil || *added.DossierId != "d1" {
		t.Fatalf("AddClaim dossier_id = %v, want d1", added.DossierId)
	}

	// Producer (petruk) cannot verify its own claim.
	if _, err := VerifyClaim(root, "d1", "claim-api", "petruk", ""); !errors.Is(err, ErrSelfVerification) {
		t.Fatalf("self-verify = %v, want ErrSelfVerification", err)
	}

	// An independent role (bagong) can.
	verified, err := VerifyClaim(root, "d1", "claim-api", "bagong", "openapi diff clean")
	if err != nil {
		t.Fatalf("VerifyClaim: %v", err)
	}
	if verified.Status != protocol.DossierClaimStatusVerified {
		t.Fatalf("verified status = %s, want verified", verified.Status)
	}
	if verified.Verification == nil || verified.Verification.Role == nil ||
		*verified.Verification.Role != protocol.DossierClaimVerificationRoleBagong {
		t.Fatalf("verification role = %+v, want bagong", verified.Verification)
	}

	// Fold-latest: Get returns one claim, at its latest (verified) state.
	loaded, err := Get(root, "d1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(loaded.Claims) != 1 {
		t.Fatalf("claims after add+verify = %d, want 1 (fold-latest)", len(loaded.Claims))
	}
	if loaded.Claims[0].Status != protocol.DossierClaimStatusVerified {
		t.Fatalf("folded claim status = %s, want verified", loaded.Claims[0].Status)
	}

	// Dispute also refuses self-verification.
	if _, err := DisputeClaim(root, "d1", "claim-api", "petruk", ""); !errors.Is(err, ErrSelfVerification) {
		t.Fatalf("self-dispute = %v, want ErrSelfVerification", err)
	}
}

func TestConformanceTotals(t *testing.T) {
	d := sampleDossier("d1") // implemented 10, partial 1, missing 0
	impl, part, miss := Conformance(d)
	if impl != 10 || part != 1 || miss != 0 {
		t.Fatalf("Conformance = (%d,%d,%d), want (10,1,0)", impl, part, miss)
	}

	// Nil plan_conformance yields all zeros, never a panic.
	var empty protocol.ChangeDossier
	if impl, part, miss := Conformance(empty); impl != 0 || part != 0 || miss != 0 {
		t.Fatalf("Conformance(empty) = (%d,%d,%d), want (0,0,0)", impl, part, miss)
	}
}

func TestExportMarkdownContainsSummaryIndicators(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleDossier("d1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := AddClaim(root, "d1", protocol.DossierClaim{
		Id:        "claim-1",
		Type:      "implementation",
		Statement: "Done.",
		Producer:  protocol.DossierClaimProducer{Role: protocol.DossierClaimProducerRolePetruk},
	}); err != nil {
		t.Fatalf("AddClaim: %v", err)
	}

	md, err := ExportMarkdown(root, "d1")
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	for _, want := range []string{
		"Requirements covered: 2 / 3",
		"Plan conformance: 10 / 11",
		"Repositories handled: 2 / 2",
		"Open contradictions: 0",
		"Verified claims: 0 / 1",
		"Blocking findings: 0",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("ExportMarkdown missing indicator %q\n---\n%s", want, md)
		}
	}
}

func TestSetContradictionsBlocksFinalize(t *testing.T) {
	root := t.TempDir()
	d := sampleDossier("d1")
	d.Status = protocol.ChangeDossierStatusVerified
	if _, err := Create(root, d); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// One unresolved contradiction should make Finalize refuse and name it.
	if _, err := SetContradictions(root, "d1", []string{"c-resolved"}, []string{"c-open"}, PutOptions{}); err != nil {
		t.Fatalf("SetContradictions: %v", err)
	}
	got, _ := Get(root, "d1")
	if got.Dossier.Contradictions == nil ||
		len(got.Dossier.Contradictions.Unresolved) != 1 ||
		got.Dossier.Contradictions.Unresolved[0] != "c-open" {
		t.Fatalf("persisted contradictions = %+v, want one unresolved c-open", got.Dossier.Contradictions)
	}

	err := Finalize(root, "d1")
	if !errors.Is(err, ErrBlockingFindings) {
		t.Fatalf("Finalize with unresolved contradiction = %v, want ErrBlockingFindings", err)
	}
	var be *BlockingError
	if !errors.As(err, &be) {
		t.Fatalf("Finalize error not *BlockingError: %v", err)
	}
	if !strings.Contains(be.Error(), "c-open") {
		t.Fatalf("blocker list %q should mention the unresolved contradiction c-open", be.Error())
	}

	// Resolving it (no unresolved) unblocks Finalize.
	if _, err := SetContradictions(root, "d1", []string{"c-resolved", "c-open"}, nil, PutOptions{}); err != nil {
		t.Fatalf("SetContradictions resolve: %v", err)
	}
	if err := Finalize(root, "d1"); err != nil {
		t.Fatalf("Finalize after resolving = %v, want nil", err)
	}
	if got, _ := Get(root, "d1"); got.Dossier.Status != protocol.ChangeDossierStatusCompleted {
		t.Fatalf("status after clean Finalize = %s, want completed", got.Dossier.Status)
	}
}

func TestSetImpactRendersInExports(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleDossier("d1")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	section := ImpactSection{
		Repositories: []string{"repo-x", "repo-y"},
		ExcludedRepositories: []ExcludedRepository{
			{Repository: "repo-legacy", Reason: "frozen, no owner"},
		},
		MissingCoverage: []string{"repo-z"},
	}
	if _, err := SetImpact(root, "d1", section, PutOptions{}); err != nil {
		t.Fatalf("SetImpact: %v", err)
	}

	got, _ := Get(root, "d1")
	if got.Dossier.Impact == nil || len(got.Dossier.Impact.Repositories) != 2 ||
		len(got.Dossier.Impact.ExcludedRepositories) != 1 {
		t.Fatalf("persisted impact = %+v", got.Dossier.Impact)
	}

	md, err := ExportMarkdown(root, "d1")
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	for _, want := range []string{
		"## Impact",
		"Repositories: repo-x",
		"Excluded repo-legacy: frozen, no owner",
		"Missing coverage: repo-z",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("ExportMarkdown missing %q\n---\n%s", want, md)
		}
	}

	pr, err := PRSummary(root, "d1")
	if err != nil {
		t.Fatalf("PRSummary: %v", err)
	}
	for _, want := range []string{"### Impact", "Excluded repo-legacy: frozen, no owner"} {
		if !strings.Contains(pr, want) {
			t.Fatalf("PRSummary missing %q\n---\n%s", want, pr)
		}
	}
}

func TestPutOptionsExpectedRevision(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleDossier("d1")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A freshly created dossier is at revision 0; a matching guard succeeds and
	// (via Put) advances the revision to 1.
	if _, err := SetImpact(root, "d1", ImpactSection{Repositories: []string{"r"}}, PutOptions{ExpectedRevision: ptr(0)}); err != nil {
		t.Fatalf("SetImpact rev 0: %v", err)
	}
	// A stale expectation now mismatches and mutates nothing.
	if _, err := SetContradictions(root, "d1", nil, []string{"c"}, PutOptions{ExpectedRevision: ptr(0)}); !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("stale revision guard = %v, want ErrRevisionMismatch", err)
	}
	if got, _ := Get(root, "d1"); got.Dossier.Contradictions != nil {
		t.Fatalf("mismatched write must not mutate; contradictions = %+v", got.Dossier.Contradictions)
	}
}

func TestExportJSONStableAndDeterministic(t *testing.T) {
	root := t.TempDir()
	if _, err := Create(root, sampleDossier("d1")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := AddEvidence(root, "d1", protocol.DossierEvidence{
		Id:   "ev-1",
		Type: protocol.DossierEvidenceTypeTestResult,
	}); err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}

	a, err := ExportJSON(root, "d1")
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	b, err := ExportJSON(root, "d1")
	if err != nil {
		t.Fatalf("ExportJSON again: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("ExportJSON not deterministic across calls")
	}
	// Valid JSON with the three top-level keys, emitted in sorted order.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(a, &top); err != nil {
		t.Fatalf("ExportJSON invalid JSON: %v", err)
	}
	for _, k := range []string{"claims", "dossier", "evidence"} {
		if _, ok := top[k]; !ok {
			t.Fatalf("ExportJSON missing key %q", k)
		}
	}
	// Sorted keys: "claims" must appear before "dossier" in the raw text.
	if strings.Index(string(a), "\"claims\"") > strings.Index(string(a), "\"dossier\"") {
		t.Fatal("ExportJSON keys not sorted (claims should precede dossier)")
	}
}
