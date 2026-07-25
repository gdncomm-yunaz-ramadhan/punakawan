package impact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func strptr(s string) *string { return &s }

func node(id string, typ protocol.ImpactNodeType) protocol.ImpactNode {
	return protocol.ImpactNode{Id: id, Type: typ}
}

func edge(from, to string, typ protocol.ImpactEdgeType, conf protocol.ImpactEdgeConfidence) protocol.ImpactEdge {
	return protocol.ImpactEdge{From: from, To: to, Type: typ, Confidence: conf}
}

func TestUpsertNodeIdempotentFoldLatest(t *testing.T) {
	root := t.TempDir()
	if err := UpsertNode(root, protocol.ImpactNode{Id: "n1", Type: protocol.ImpactNodeTypeSourceSymbol, Label: strptr("first")}); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	if err := UpsertNode(root, protocol.ImpactNode{Id: "n1", Type: protocol.ImpactNodeTypeSourceSymbol, Label: strptr("second")}); err != nil {
		t.Fatalf("UpsertNode 2: %v", err)
	}
	nodes, err := Nodes(root)
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("folded node count = %d, want 1", len(nodes))
	}
	if nodes[0].Label == nil || *nodes[0].Label != "second" {
		t.Fatalf("fold-latest failed: label = %v, want second", nodes[0].Label)
	}
}

func TestUpsertEdgeIdempotentFoldLatest(t *testing.T) {
	root := t.TempDir()
	if err := UpsertEdge(root, edge("a", "b", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceInferred)); err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	// Same (from,to,type) with upgraded confidence supersedes the prior line.
	if err := UpsertEdge(root, edge("a", "b", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceVerified)); err != nil {
		t.Fatalf("UpsertEdge 2: %v", err)
	}
	// A different type between the same nodes is a distinct edge.
	if err := UpsertEdge(root, edge("a", "b", protocol.ImpactEdgeTypeDependsOn, protocol.ImpactEdgeConfidenceObserved)); err != nil {
		t.Fatalf("UpsertEdge 3: %v", err)
	}
	edges, err := Edges(root)
	if err != nil {
		t.Fatalf("Edges: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("folded edge count = %d, want 2", len(edges))
	}
	if edges[0].Confidence != protocol.ImpactEdgeConfidenceVerified {
		t.Fatalf("confidence not preserved/folded: %s, want verified", edges[0].Confidence)
	}
}

func TestGetNode(t *testing.T) {
	root := t.TempDir()
	_ = UpsertNode(root, node("n1", protocol.ImpactNodeTypeTest))
	if got, ok, err := GetNode(root, "n1"); err != nil || !ok || got.Type != protocol.ImpactNodeTypeTest {
		t.Fatalf("GetNode(n1) = %+v, %v, %v", got, ok, err)
	}
	if _, ok, err := GetNode(root, "missing"); err != nil || ok {
		t.Fatalf("GetNode(missing) ok = %v (err %v), want false", ok, err)
	}
}

func TestQueryDirectAndTransitive(t *testing.T) {
	root := t.TempDir()
	// S -> A (direct) -> B (transitive)
	for _, n := range []protocol.ImpactNode{
		node("S", protocol.ImpactNodeTypeSourceSymbol),
		node("A", protocol.ImpactNodeTypeSourceSymbol),
		node("B", protocol.ImpactNodeTypeSourceSymbol),
	} {
		if err := UpsertNode(root, n); err != nil {
			t.Fatalf("UpsertNode: %v", err)
		}
	}
	_ = UpsertEdge(root, edge("S", "A", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceObserved))
	_ = UpsertEdge(root, edge("A", "B", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceObserved))

	// depth 1: only A is reached.
	r1, err := Query(root, "S", 1, nil)
	if err != nil {
		t.Fatalf("Query depth1: %v", err)
	}
	if len(r1.DirectImpact) != 1 || r1.DirectImpact[0].Id != "A" {
		t.Fatalf("depth1 DirectImpact = %+v, want [A]", r1.DirectImpact)
	}
	if len(r1.TransitiveImpact) != 0 {
		t.Fatalf("depth1 TransitiveImpact = %+v, want none", r1.TransitiveImpact)
	}

	// depth 2: B appears as transitive.
	r2, err := Query(root, "S", 2, nil)
	if err != nil {
		t.Fatalf("Query depth2: %v", err)
	}
	if len(r2.DirectImpact) != 1 || r2.DirectImpact[0].Id != "A" {
		t.Fatalf("depth2 DirectImpact = %+v, want [A]", r2.DirectImpact)
	}
	if len(r2.TransitiveImpact) != 1 || r2.TransitiveImpact[0].Id != "B" {
		t.Fatalf("depth2 TransitiveImpact = %+v, want [B]", r2.TransitiveImpact)
	}
}

func TestQueryCycleSafe(t *testing.T) {
	root := t.TempDir()
	_ = UpsertNode(root, node("A", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertNode(root, node("B", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertEdge(root, edge("A", "B", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceObserved))
	_ = UpsertEdge(root, edge("B", "A", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceObserved))

	// A large depth must still terminate thanks to the visited set.
	r, err := Query(root, "A", 100, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(r.DirectImpact) != 1 || r.DirectImpact[0].Id != "B" {
		t.Fatalf("DirectImpact = %+v, want [B]", r.DirectImpact)
	}
	if len(r.TransitiveImpact) != 0 {
		t.Fatalf("TransitiveImpact = %+v, want none (A is the subject)", r.TransitiveImpact)
	}
}

func TestQueryIncomingEdgesTraversed(t *testing.T) {
	root := t.TempDir()
	_ = UpsertNode(root, node("S", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertNode(root, node("Caller", protocol.ImpactNodeTypeSourceSymbol))
	// Caller -> S. Changing S affects Caller (incoming edge from S's view).
	_ = UpsertEdge(root, edge("Caller", "S", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceObserved))

	r, err := Query(root, "S", 1, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(r.DirectImpact) != 1 || r.DirectImpact[0].Id != "Caller" {
		t.Fatalf("DirectImpact = %+v, want [Caller]", r.DirectImpact)
	}
}

func TestQueryDisputedEdgeSurfaced(t *testing.T) {
	root := t.TempDir()
	_ = UpsertNode(root, node("S", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertNode(root, node("X", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertEdge(root, edge("S", "X", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceDisputed))

	r, err := Query(root, "S", 1, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(r.RelatedContradictions) != 1 || r.RelatedContradictions[0] != "X" {
		t.Fatalf("RelatedContradictions = %+v, want [X]", r.RelatedContradictions)
	}
}

func TestQueryMissingCoverage(t *testing.T) {
	root := t.TempDir()
	// S (covered by a test) -> Uncovered (a symbol with no test).
	_ = UpsertNode(root, node("S", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertNode(root, node("Uncovered", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertNode(root, node("T", protocol.ImpactNodeTypeTest))
	_ = UpsertEdge(root, edge("S", "Uncovered", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceObserved))
	_ = UpsertEdge(root, edge("T", "S", protocol.ImpactEdgeTypeTests, protocol.ImpactEdgeConfidenceObserved))

	r, err := Query(root, "S", 2, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// S has an incoming tests edge; Uncovered does not.
	if len(r.MissingCoverage) != 1 || r.MissingCoverage[0].Id != "Uncovered" {
		t.Fatalf("MissingCoverage = %+v, want [Uncovered]", r.MissingCoverage)
	}
	// T is reached via S's incoming edge and must appear as an affected test.
	foundTest := false
	for _, tn := range r.AffectedTests {
		if tn.Id == "T" {
			foundTest = true
		}
	}
	if !foundTest {
		t.Fatalf("AffectedTests = %+v, want to include T", r.AffectedTests)
	}
}

func TestQueryIncludeTypesFilter(t *testing.T) {
	root := t.TempDir()
	// S -> A (source_symbol, direct) -> Dep (deployment_artifact, transitive)
	//                                 -> Sym (source_symbol, transitive)
	_ = UpsertNode(root, node("S", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertNode(root, node("A", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertNode(root, node("Dep", protocol.ImpactNodeTypeDeploymentArtifact))
	_ = UpsertNode(root, node("Sym", protocol.ImpactNodeTypeSourceSymbol))
	_ = UpsertEdge(root, edge("S", "A", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceObserved))
	_ = UpsertEdge(root, edge("A", "Dep", protocol.ImpactEdgeTypeDeploys, protocol.ImpactEdgeConfidenceObserved))
	_ = UpsertEdge(root, edge("A", "Sym", protocol.ImpactEdgeTypeCalls, protocol.ImpactEdgeConfidenceObserved))

	r, err := Query(root, "S", 2, []string{string(protocol.ImpactNodeTypeDeploymentArtifact)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// Transitive filtered to deployment_artifact only.
	if len(r.TransitiveImpact) != 1 || r.TransitiveImpact[0].Id != "Dep" {
		t.Fatalf("filtered TransitiveImpact = %+v, want [Dep]", r.TransitiveImpact)
	}
	// Aggregates are unaffected by the transitive filter.
	if len(r.DeploymentArtifacts) != 1 || r.DeploymentArtifacts[0].Id != "Dep" {
		t.Fatalf("DeploymentArtifacts = %+v, want [Dep]", r.DeploymentArtifacts)
	}
}

func TestBuildFromWorkspaceCreatesProjectAndRepoNodes(t *testing.T) {
	root := t.TempDir()
	punakawanDir := filepath.Join(root, ".punakawan")
	if err := os.MkdirAll(punakawanDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ws := "version: punakawan.workspace/v1\nid: shop\nname: Shop\nrepositories:\n  - id: repo-a\n    path: ./repo-a\n  - id: repo-b\n    path: ./repo-b\n"
	if err := os.WriteFile(filepath.Join(punakawanDir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}

	if err := BuildFromWorkspace(root); err != nil {
		t.Fatalf("BuildFromWorkspace: %v", err)
	}
	// Refresh is idempotent: a second run must not duplicate anything.
	if err := Refresh(root); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	nodes, err := Nodes(root)
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	// 1 project + 2 repositories.
	if len(nodes) != 3 {
		t.Fatalf("node count = %d, want 3 (idempotent build): %+v", len(nodes), nodes)
	}
	if _, ok, _ := GetNode(root, "project:shop"); !ok {
		t.Errorf("missing project node project:shop")
	}
	for _, id := range []string{"repository:repo-a", "repository:repo-b"} {
		n, ok, _ := GetNode(root, id)
		if !ok {
			t.Errorf("missing repository node %s", id)
			continue
		}
		if n.Type != protocol.ImpactNodeTypeRepository {
			t.Errorf("%s type = %s, want repository", id, n.Type)
		}
	}

	edges, err := Edges(root)
	if err != nil {
		t.Fatalf("Edges: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("edge count = %d, want 2 contains edges (idempotent): %+v", len(edges), edges)
	}
	for _, e := range edges {
		if e.Type != protocol.ImpactEdgeTypeContains || e.From != "project:shop" {
			t.Errorf("unexpected edge %+v", e)
		}
	}
}

func TestStubBuildersReturnNil(t *testing.T) {
	root := t.TempDir()
	// Run outside any workspace: the on-disk scanners must degrade to no-ops
	// (no workspace discoverable) rather than erroring, and the remaining
	// stubs must return nil, and none may write any data.
	for name, fn := range map[string]func(string) error{
		"openapi": BuildFromOpenAPI,
		"tests":   BuildFromTests,
		"config":  BuildFromConfig,
		"deploy":  BuildFromDeploy,
		"sources": BuildFromSources,
	} {
		if err := fn(root); err != nil {
			t.Errorf("%s builder returned %v, want nil", name, err)
		}
	}
	nodes, _ := Nodes(root)
	if len(nodes) != 0 {
		t.Fatalf("builders wrote %d nodes, want 0 (no workspace)", len(nodes))
	}
}

// TestBuildFromWorkspaceScansOnDiskSources creates a small single-repo
// workspace with an OpenAPI spec, a test file, and a Dockerfile, then asserts
// BuildFromWorkspace populates the graph with the expected node/edge types and
// that Query surfaces them.
func TestBuildFromWorkspaceScansOnDiskSources(t *testing.T) {
	root := t.TempDir()
	punakawanDir := filepath.Join(root, ".punakawan")
	if err := os.MkdirAll(punakawanDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Single repo rooted at the workspace root itself.
	ws := "version: punakawan.workspace/v1\nid: shop\nname: Shop\nrepositories:\n  - id: api\n    path: .\n"
	if err := os.WriteFile(filepath.Join(punakawanDir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
		t.Fatalf("write workspace.yaml: %v", err)
	}

	// OpenAPI spec with one operation.
	openapi := "openapi: 3.0.0\ninfo:\n  title: Shop\n  version: 1.0.0\npaths:\n  /merchants/{id}/badge:\n    get:\n      operationId: getMerchantBadge\n      summary: Get badge\n"
	if err := os.WriteFile(filepath.Join(root, "openapi.yaml"), []byte(openapi), 0o644); err != nil {
		t.Fatalf("write openapi: %v", err)
	}
	// A Go source file and its sibling test.
	if err := os.WriteFile(filepath.Join(root, "merchant.go"), []byte("package shop\n\nfunc Badge() {}\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "merchant_test.go"), []byte("package shop\n\nimport \"testing\"\n\nfunc TestBadge(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatalf("write test: %v", err)
	}
	// A Dockerfile (deployment artifact).
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write dockerfile: %v", err)
	}

	if err := BuildFromWorkspace(root); err != nil {
		t.Fatalf("BuildFromWorkspace: %v", err)
	}
	// Idempotent: a second pass must not change the folded graph.
	if err := Refresh(root); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	nodes, err := Nodes(root)
	if err != nil {
		t.Fatalf("Nodes: %v", err)
	}
	byType := map[protocol.ImpactNodeType]int{}
	for _, n := range nodes {
		byType[n.Type]++
	}
	for _, want := range []protocol.ImpactNodeType{
		protocol.ImpactNodeTypeProject,
		protocol.ImpactNodeTypeRepository,
		protocol.ImpactNodeTypeApiOperation,
		protocol.ImpactNodeTypeTest,
		protocol.ImpactNodeTypeSourceSymbol,
		protocol.ImpactNodeTypeDeploymentArtifact,
	} {
		if byType[want] == 0 {
			t.Errorf("no node of type %s produced; nodes=%+v", want, nodes)
		}
	}

	// The OpenAPI operation node uses its operationId key.
	apiID := apiNodeID("api", "getMerchantBadge")
	if _, ok, _ := GetNode(root, apiID); !ok {
		t.Errorf("missing api_operation node %s", apiID)
	}
	// The Dockerfile deployment artifact node.
	deployID := deployNodeID("api", "Dockerfile")
	if _, ok, _ := GetNode(root, deployID); !ok {
		t.Errorf("missing deployment_artifact node %s", deployID)
	}

	// Edge types produced by the scanners are present.
	edges, err := Edges(root)
	if err != nil {
		t.Fatalf("Edges: %v", err)
	}
	haveEdge := map[protocol.ImpactEdgeType]bool{}
	for _, e := range edges {
		haveEdge[e.Type] = true
	}
	for _, want := range []protocol.ImpactEdgeType{
		protocol.ImpactEdgeTypeDocumentedBy,
		protocol.ImpactEdgeTypeTests,
		protocol.ImpactEdgeTypeConfigures,
	} {
		if !haveEdge[want] {
			t.Errorf("no edge of type %s produced; edges=%+v", want, edges)
		}
	}

	// Query from the repository reaches the scanned artifacts.
	res, err := Query(root, repositoryNodeID("api"), 3, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.DeploymentArtifacts) == 0 {
		t.Errorf("Query DeploymentArtifacts empty, want the Dockerfile artifact")
	}
	foundAPI := false
	for _, n := range append(res.DirectImpact, res.TransitiveImpact...) {
		if n.Id == apiID {
			foundAPI = true
		}
	}
	if !foundAPI {
		t.Errorf("Query did not reach api_operation %s; direct=%+v transitive=%+v", apiID, res.DirectImpact, res.TransitiveImpact)
	}
}

// TestInjectedDataBuilders exercises the DI adapters that other packages feed.
func TestInjectedDataBuilders(t *testing.T) {
	root := t.TempDir()
	if err := UpsertNode(root, node("api:shop:getBadge", protocol.ImpactNodeTypeApiOperation)); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if err := BuildFromKnowledge(root, []KnowledgeRef{
		{ID: "k1", Title: "Badge rules", Relates: []string{"api:shop:getBadge"}},
	}); err != nil {
		t.Fatalf("BuildFromKnowledge: %v", err)
	}
	if err := BuildFromPlanTasks(root, []PlanTaskRef{
		{ID: "t1", Kind: "task", Title: "Ship badge", Tracks: []string{"api:shop:getBadge"}},
	}); err != nil {
		t.Fatalf("BuildFromPlanTasks: %v", err)
	}
	if _, ok, _ := GetNode(root, "knowledge:k1"); !ok {
		t.Errorf("missing knowledge_record node knowledge:k1")
	}
	if _, ok, _ := GetNode(root, "task:t1"); !ok {
		t.Errorf("missing task node task:t1")
	}
	// The knowledge record and task are reachable from the operation.
	res, err := Query(root, "api:shop:getBadge", 2, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	got := map[string]bool{}
	for _, n := range append(res.DirectImpact, res.TransitiveImpact...) {
		got[n.Id] = true
	}
	if !got["knowledge:k1"] || !got["task:t1"] {
		t.Errorf("Query did not reach injected nodes; reached=%v", got)
	}
}
