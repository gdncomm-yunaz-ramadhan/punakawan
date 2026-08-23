// Package scenario holds the plan's §68 end-to-end scenario as an executable
// test that exercises all four distinguished subsystems together against real
// on-disk stores in a temp project root. It is the integration proof that the
// per-subsystem stores compose into the intended feature-delivery flow:
//
//  1. Semar starts feature-delivery (role config resolves to defaults).
//  2. Gareng detects a blocking contradiction (conflicting retry values).
//  3. A dossier cannot be finalized while that contradiction is unresolved.
//  4. Gareng maps API/UI/E2E impact; the graph answers direct+transitive queries.
//  5. The user resolves the contradiction.
//  6. Petruk records implementation claims; Bagong independently verifies them.
//  7. Bagong marks repo-ui a deliberate, reasoned exclusion.
//  8. The dossier reaches verified and finalizes clean.
package scenario

import (
	"errors"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/contradiction"
	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/internal/impact"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func strptr(s string) *string { return &s }

func TestFeatureDeliveryEndToEnd(t *testing.T) {
	root := t.TempDir()
	const projectID = "affiliate-platform"

	// 1. Semar starts feature-delivery: role config resolves (defaults are
	//    synthesized on a project that has never configured roles).
	if _, err := roleconfig.Load(root); err != nil {
		t.Fatalf("load role config: %v", err)
	}

	// 2. Gareng detects conflicting retry values across two sources: a blocking,
	//    critical contradiction.
	subjectKey := "payout.retry.max_attempts"
	blocking := true
	con := protocol.Contradiction{
		Id:        "contra-retry",
		ProjectId: projectID,
		Title:     "Conflicting payout retry max between repo-api and Confluence",
		Severity:  protocol.ContradictionSeverityCritical,
		Status:    protocol.ContradictionStatusDetected,
		Blocking:  &blocking,
		Subject: protocol.ContradictionSubject{
			Type: protocol.ContradictionSubjectTypeConfiguration,
			Key:  &subjectKey,
		},
		Claims: []protocol.ContradictionClaimsElem{
			{Source: protocol.ContradictionClaimsElemSource{Type: protocol.ContradictionClaimsElemSourceTypeRepository}, Statement: "max_attempts = 3"},
			{Source: protocol.ContradictionClaimsElemSource{Type: protocol.ContradictionClaimsElemSourceTypeConfluence}, Statement: "max_attempts = 5"},
		},
	}
	if err := contradiction.Put(root, con, contradiction.PutOptions{}); err != nil {
		t.Fatalf("put contradiction: %v", err)
	}
	if open, err := contradiction.OpenBlocking(root); err != nil || len(open) != 1 {
		t.Fatalf("OpenBlocking = %v (err %v), want exactly 1", open, err)
	}

	// 3. Semar opens the change dossier and records the unresolved contradiction.
	//    While it is unresolved, the dossier must not finalize.
	d, err := dossier.Create(root, protocol.ChangeDossier{
		Id:        "dossier-feature",
		ProjectId: projectID,
		Title:     "Unify payout retry policy",
		Objective: protocol.ChangeDossierObjective{Statement: "Make payout retry consistent across API, UI, and E2E."},
	})
	if err != nil {
		t.Fatalf("create dossier: %v", err)
	}
	if _, err := dossier.SetContradictions(root, d.Id, nil, []string{con.Id}, dossier.PutOptions{}); err != nil {
		t.Fatalf("set contradictions: %v", err)
	}
	// Drive the dossier up to verified so finalize's only remaining gate is the
	// blocking finding, then confirm the unresolved contradiction blocks it.
	for _, to := range []protocol.ChangeDossierStatus{
		protocol.ChangeDossierStatusContextReady,
		protocol.ChangeDossierStatusPlanned,
		protocol.ChangeDossierStatusImplementing,
		protocol.ChangeDossierStatusAwaitingVerification,
		protocol.ChangeDossierStatusVerified,
	} {
		if err := dossier.Advance(root, d.Id, to); err != nil {
			t.Fatalf("advance dossier to %s: %v", to, err)
		}
	}
	err = dossier.Finalize(root, d.Id)
	if !errors.Is(err, dossier.ErrBlockingFindings) {
		t.Fatalf("Finalize with unresolved contradiction = %v, want ErrBlockingFindings", err)
	}

	// 4. Gareng maps impact: repo-api owns the config; repo-e2e tests it; repo-ui
	//    is reachable but ultimately excluded. Edges carry evidence/confidence.
	nodes := []protocol.ImpactNode{
		{Id: "repository:repo-api", Type: protocol.ImpactNodeTypeRepository, Repository: strptr("repo-api")},
		{Id: "repository:repo-ui", Type: protocol.ImpactNodeTypeRepository, Repository: strptr("repo-ui")},
		{Id: "repository:repo-e2e", Type: protocol.ImpactNodeTypeRepository, Repository: strptr("repo-e2e")},
		{Id: "config:repo-api:" + subjectKey, Type: protocol.ImpactNodeTypeConfigurationKey, Repository: strptr("repo-api")},
		{Id: "test:repo-e2e:payout", Type: protocol.ImpactNodeTypeTest, Repository: strptr("repo-e2e")},
	}
	for _, n := range nodes {
		if err := impact.UpsertNode(root, n); err != nil {
			t.Fatalf("upsert node %s: %v", n.Id, err)
		}
	}
	edges := []protocol.ImpactEdge{
		{From: "repository:repo-api", To: "config:repo-api:" + subjectKey, Type: protocol.ImpactEdgeTypeConfigures, Confidence: protocol.ImpactEdgeConfidenceObserved},
		{From: "test:repo-e2e:payout", To: "config:repo-api:" + subjectKey, Type: protocol.ImpactEdgeTypeTests, Confidence: protocol.ImpactEdgeConfidenceObserved},
	}
	for _, e := range edges {
		if err := impact.UpsertEdge(root, e); err != nil {
			t.Fatalf("upsert edge %s->%s: %v", e.From, e.To, err)
		}
	}
	res, err := impact.Query(root, "config:repo-api:"+subjectKey, 3, nil)
	if err != nil {
		t.Fatalf("impact query: %v", err)
	}
	if !containsStr(res.AffectedRepositories, "repo-api") || !containsStr(res.AffectedRepositories, "repo-e2e") {
		t.Fatalf("affected repos = %v, want repo-api and repo-e2e", res.AffectedRepositories)
	}
	if containsStr(res.AffectedRepositories, "repo-ui") {
		t.Fatalf("repo-ui should not be impacted by the retry config: %v", res.AffectedRepositories)
	}

	// 5. The user resolves the contradiction, walking §18's lifecycle chain
	//    (detected -> triaged -> needs_clarification -> proposed -> resolved).
	for _, to := range []protocol.ContradictionStatus{
		protocol.ContradictionStatusTriaged,
		protocol.ContradictionStatusNeedsClarification,
	} {
		c, err := contradiction.Get(root, con.Id)
		if err != nil {
			t.Fatalf("get contradiction: %v", err)
		}
		if err := contradiction.Transition(c, to); err != nil {
			t.Fatalf("transition contradiction to %s: %v", to, err)
		}
		if err := contradiction.Put(root, *c, contradiction.PutOptions{}); err != nil {
			t.Fatalf("put contradiction at %s: %v", to, err)
		}
	}
	if err := contradiction.ProposeResolution(root, con.Id, "Standardize on max_attempts = 5.", "Confluence is authoritative.", true); err != nil {
		t.Fatalf("propose resolution: %v", err)
	}
	if err := contradiction.Resolve(root, con.Id, "Standardize on max_attempts = 5 (Confluence is authoritative).", "user"); err != nil {
		t.Fatalf("resolve contradiction: %v", err)
	}
	if open, err := contradiction.OpenBlocking(root); err != nil || len(open) != 0 {
		t.Fatalf("OpenBlocking after resolve = %v (err %v), want empty", open, err)
	}
	if _, err := dossier.SetContradictions(root, d.Id, []string{con.Id}, nil, dossier.PutOptions{}); err != nil {
		t.Fatalf("move contradiction to resolved on dossier: %v", err)
	}

	// 6. Petruk records an implementation claim; Bagong (an independent role)
	//    verifies it. A role verifying its own claim is rejected (§34).
	claim, err := dossier.AddClaim(root, d.Id, protocol.DossierClaim{
		Id:        "claim-impl",
		Type:      "implementation",
		Statement: "repo-api and repo-e2e updated to max_attempts = 5.",
		Producer:  protocol.DossierClaimProducer{Role: protocol.DossierClaimProducerRolePetruk},
		Status:    protocol.DossierClaimStatusSupported,
	})
	if err != nil {
		t.Fatalf("add claim: %v", err)
	}
	if _, err := dossier.VerifyClaim(root, d.Id, claim.Id, string(protocol.DossierClaimProducerRolePetruk), "self"); !errors.Is(err, dossier.ErrSelfVerification) {
		t.Fatalf("self-verify = %v, want ErrSelfVerification", err)
	}
	if _, err := dossier.VerifyClaim(root, d.Id, claim.Id, string(protocol.DossierClaimProducerRoleBagong), "independently confirmed"); err != nil {
		t.Fatalf("bagong verify: %v", err)
	}

	// 7. Bagong marks repo-ui a deliberate, reasoned exclusion.
	if _, err := dossier.SetImpact(root, d.Id, dossier.ImpactSection{
		Repositories: []string{"repo-api", "repo-e2e"},
		ExcludedRepositories: []dossier.ExcludedRepository{
			{Repository: "repo-ui", Reason: "UI reads the retry policy from the API at runtime; no UI change needed."},
		},
	}, dossier.PutOptions{}); err != nil {
		t.Fatalf("set impact: %v", err)
	}

	// 8. The dossier finalizes clean, and the exclusion + reason are exportable.
	if err := dossier.Finalize(root, d.Id); err != nil {
		t.Fatalf("Finalize clean dossier: %v", err)
	}
	md, err := dossier.ExportMarkdown(root, d.Id)
	if err != nil {
		t.Fatalf("export markdown: %v", err)
	}
	if !strings.Contains(md, "repo-ui") || !strings.Contains(md, "no UI change needed") {
		t.Fatalf("markdown export missing repo-ui exclusion reason:\n%s", md)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
