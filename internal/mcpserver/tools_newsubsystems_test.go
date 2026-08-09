package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/internal/impact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// TestSubmitContradictionCreatesThenDedups checks a new contradiction is
// recorded and a second submission for the same normalized subject key returns
// the existing record rather than duplicating it (§20/CONTRA-012).
func TestSubmitContradictionCreatesThenDedups(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	// A contradiction needs at least two conflicting claims (§16).
	claims := []any{
		map[string]any{
			"source":    map[string]any{"type": "repository", "ref": "config/app.yaml"},
			"statement": "max attempts is 3",
		},
		map[string]any{
			"source":    map[string]any{"type": "jira", "ref": "TRF-1"},
			"statement": "max attempts is 5",
		},
	}
	var first SubmitContradictionOutput
	callTool(t, cs, "submit_contradiction", map[string]any{
		"title":    "payout retry attempts disagree",
		"severity": "material",
		"subject":  map[string]any{"type": "configuration", "key": "Payout.Retry.Max_Attempts"},
		"claims":   claims,
	}, &first)

	if first.Deduplicated {
		t.Fatal("first submission should not be a dedup hit")
	}
	if first.Contradiction.Id == "" {
		t.Fatal("first submission returned no id")
	}
	if first.Contradiction.Status != protocol.ContradictionStatusDetected {
		t.Fatalf("status = %q, want detected", first.Contradiction.Status)
	}

	// A differently-punctuated/cased key normalizes to the same value and must
	// dedup onto the first record.
	var second SubmitContradictionOutput
	callTool(t, cs, "submit_contradiction", map[string]any{
		"title":    "duplicate detection",
		"severity": "material",
		"subject":  map[string]any{"type": "configuration", "key": "payout retry max attempts"},
		"claims":   claims,
	}, &second)

	if !second.Deduplicated {
		t.Fatal("second submission should be a dedup hit")
	}
	if second.Contradiction.Id != first.Contradiction.Id {
		t.Fatalf("dedup id = %q, want %q", second.Contradiction.Id, first.Contradiction.Id)
	}
}

// TestAnalyzeImpactReturnsResult builds the structural spine from the workspace
// and checks a query from the project node reports the contained repository.
func TestAnalyzeImpactReturnsResult(t *testing.T) {
	a := newTestApp(t)
	if err := impact.BuildFromWorkspace(a.Workspace.Root); err != nil {
		t.Fatalf("BuildFromWorkspace: %v", err)
	}
	cs := connect(t, a)

	var res AnalyzeImpactOutput
	callTool(t, cs, "analyze_impact", map[string]any{
		"subject_id": "project:" + a.Workspace.ID,
		"depth":      3,
	}, &res)

	found := false
	for _, r := range res.AffectedRepositories {
		if r == "repo-a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("AffectedRepositories = %v, want to include repo-a", res.AffectedRepositories)
	}
	if len(res.DirectImpact) == 0 {
		t.Fatalf("DirectImpact is empty, want the repository node")
	}
}

// TestFinalizeDossierBlockedThenClean checks Finalize refuses while a claim is
// disputed and succeeds once the dispute is cleared by verification.
func TestFinalizeDossierBlockedThenClean(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var created ChangeDossierOutput
	callTool(t, cs, "create_change_dossier", map[string]any{
		"id":        "d1",
		"title":     "refund flow change",
		"objective": map[string]any{"statement": "ship refunds"},
	}, &created)
	if created.Dossier.Id != "d1" {
		t.Fatalf("dossier id = %q, want d1", created.Dossier.Id)
	}

	// Walk the lifecycle to verified so Finalize is only gated by blocking
	// findings, not the status edge (there is no Advance MCP tool).
	for _, to := range []protocol.ChangeDossierStatus{
		protocol.ChangeDossierStatusContextReady,
		protocol.ChangeDossierStatusPlanned,
		protocol.ChangeDossierStatusImplementing,
		protocol.ChangeDossierStatusAwaitingVerification,
		protocol.ChangeDossierStatusVerified,
	} {
		if err := dossier.Advance(a.Workspace.Root, "d1", to); err != nil {
			t.Fatalf("Advance to %s: %v", to, err)
		}
	}

	var claim DossierClaimOutput
	callTool(t, cs, "add_dossier_claim", map[string]any{
		"dossier_id": "d1",
		"claim": map[string]any{
			"id":        "c1",
			"producer":  map[string]any{"role": "gareng"},
			"type":      "implementation",
			"statement": "implementation matches the plan",
		},
	}, &claim)

	// Dispute by an independent role -> a blocking finding.
	var disputed DossierClaimOutput
	callTool(t, cs, "dispute_dossier_claim", map[string]any{
		"dossier_id": "d1",
		"claim_id":   "c1",
		"by_role":    "bagong",
	}, &disputed)
	if disputed.Claim.Status != protocol.DossierClaimStatusDisputed {
		t.Fatalf("claim status = %q, want disputed", disputed.Claim.Status)
	}

	// Finalize must be blocked while the dispute stands.
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "finalize_dossier",
		Arguments: map[string]any{"dossier_id": "d1"},
	})
	if err != nil {
		t.Fatalf("CallTool finalize (blocked): %v", err)
	}
	if !res.IsError {
		t.Fatal("finalize should be blocked by the disputed claim")
	}

	// Clear the dispute with a verification (latest claim line wins).
	var verified DossierClaimOutput
	callTool(t, cs, "verify_dossier_claim", map[string]any{
		"dossier_id": "d1",
		"claim_id":   "c1",
		"by_role":    "bagong",
	}, &verified)
	if verified.Claim.Status != protocol.DossierClaimStatusVerified {
		t.Fatalf("claim status = %q, want verified", verified.Claim.Status)
	}

	// Now finalize cleanly.
	var final ChangeDossierOutput
	callTool(t, cs, "finalize_dossier", map[string]any{"dossier_id": "d1"}, &final)
	if final.Dossier.Status != protocol.ChangeDossierStatusCompleted {
		t.Fatalf("finalized status = %q, want completed", final.Dossier.Status)
	}
}

// TestSemarFinalizeBlockedByOpenContradiction checks CONTRA-008: Semar cannot
// submit a final plan while a blocking contradiction is still open. A critical
// contradiction is blocking by default (§19), so submitting one and then a
// final_plan must be refused with a message naming the blocker.
func TestSemarFinalizeBlockedByOpenContradiction(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var con SubmitContradictionOutput
	callTool(t, cs, "submit_contradiction", map[string]any{
		"title":    "critical retry disagreement",
		"severity": "critical",
		"subject":  map[string]any{"type": "configuration", "key": "payout.retry.max"},
		"claims": []any{
			map[string]any{"source": map[string]any{"type": "repository", "ref": "a"}, "statement": "3"},
			map[string]any{"source": map[string]any{"type": "jira", "ref": "b"}, "statement": "5"},
		},
	}, &con)
	if con.Contradiction.Blocking == nil || !*con.Contradiction.Blocking {
		t.Fatalf("a critical contradiction should be blocking by default: %+v", con.Contradiction)
	}

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "submit_semar_synthesis",
		Arguments: map[string]any{
			"id":         "run-1",
			"title":      "final plan",
			"final_plan": map[string]any{"requirements": []string{"r1"}, "acceptance_criteria": []string{"a1"}},
		},
	})
	if err != nil {
		t.Fatalf("CallTool submit_semar_synthesis: %v", err)
	}
	if !res.IsError {
		t.Fatal("final plan submission should be refused while a blocking contradiction is open")
	}
	msg := errorText(res)
	if !strings.Contains(msg, "blocking contradiction") || !strings.Contains(msg, con.Contradiction.Id) {
		t.Fatalf("error %q should name the blocking contradiction %q", msg, con.Contradiction.Id)
	}
}

// errorText concatenates the text content blocks of an error tool result.
func errorText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// TestVerifyImpactCoverageReportsGaps checks IMPACT-014: an impacted symbol
// with no incoming `tests` edge is reported as missing coverage (covered=false),
// and once a test covers it the verdict flips to covered.
func TestVerifyImpactCoverageReportsGaps(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)
	root := a.Workspace.Root

	repo := "repo-api"
	sym := "symbol:repo-api:PayoutService"
	cfg := "config:repo-api:payout.retry"
	for _, n := range []protocol.ImpactNode{
		{Id: sym, Type: protocol.ImpactNodeTypeSourceSymbol, Repository: &repo},
		{Id: cfg, Type: protocol.ImpactNodeTypeConfigurationKey, Repository: &repo},
	} {
		if err := impact.UpsertNode(root, n); err != nil {
			t.Fatalf("upsert node: %v", err)
		}
	}
	if err := impact.UpsertEdge(root, protocol.ImpactEdge{From: cfg, To: sym, Type: protocol.ImpactEdgeTypeConfigures, Confidence: protocol.ImpactEdgeConfidenceObserved}); err != nil {
		t.Fatalf("upsert edge: %v", err)
	}

	var gap VerifyImpactCoverageOutput
	callTool(t, cs, "verify_impact_coverage", map[string]any{"subject_id": cfg}, &gap)
	if gap.Covered {
		t.Fatalf("expected covered=false while %s has no test", sym)
	}
	if len(gap.MissingCoverage) == 0 {
		t.Fatal("expected the untested symbol in missing_coverage")
	}

	// Add a test covering the symbol; coverage should now be complete.
	test := "test:repo-api:PayoutServiceTest"
	if err := impact.UpsertNode(root, protocol.ImpactNode{Id: test, Type: protocol.ImpactNodeTypeTest, Repository: &repo}); err != nil {
		t.Fatalf("upsert test node: %v", err)
	}
	if err := impact.UpsertEdge(root, protocol.ImpactEdge{From: test, To: sym, Type: protocol.ImpactEdgeTypeTests, Confidence: protocol.ImpactEdgeConfidenceObserved}); err != nil {
		t.Fatalf("upsert tests edge: %v", err)
	}
	var covered VerifyImpactCoverageOutput
	callTool(t, cs, "verify_impact_coverage", map[string]any{"subject_id": cfg}, &covered)
	if !covered.Covered {
		t.Fatalf("expected covered=true after adding a test; missing=%v", covered.MissingCoverage)
	}
}

// TestSubmitContradictionMergesLinks checks CONTRA-011 wiring: caller-supplied
// entity links are persisted on the stored contradiction.
func TestSubmitContradictionMergesLinks(t *testing.T) {
	a := newTestApp(t)
	cs := connect(t, a)

	var out SubmitContradictionOutput
	callTool(t, cs, "submit_contradiction", map[string]any{
		"title":    "retry disagreement with links",
		"severity": "material",
		"subject":  map[string]any{"type": "configuration", "key": "payout.retry.links"},
		"claims": []any{
			map[string]any{"source": map[string]any{"type": "repository", "ref": "repo-api"}, "statement": "3"},
			map[string]any{"source": map[string]any{"type": "jira", "ref": "TRF-9"}, "statement": "5"},
		},
		"links": map[string]any{"plans": []string{"plan-42"}, "repositories": []string{"repo-api"}},
	}, &out)

	if out.Contradiction.Links == nil {
		t.Fatal("expected links on the stored contradiction")
	}
	if !containsStr(out.Contradiction.Links.Plans, "plan-42") {
		t.Fatalf("plans = %v, want plan-42", out.Contradiction.Links.Plans)
	}
	if !containsStr(out.Contradiction.Links.Repositories, "repo-api") {
		t.Fatalf("repositories = %v, want repo-api", out.Contradiction.Links.Repositories)
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
