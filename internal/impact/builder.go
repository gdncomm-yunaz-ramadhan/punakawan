package impact

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ygrip/punakawan/internal/workspace"
	"github.com/ygrip/punakawan/pkg/protocol"
	"gopkg.in/yaml.v3"
)

// Builder id conventions. Ids are stable and typed so every builder can upsert
// idempotently (§24): re-running a build re-emits the same ids and the JSONL
// fold keeps the graph converged rather than growing duplicates.
func projectNodeID(id string) string         { return "project:" + id }
func repositoryNodeID(id string) string      { return "repository:" + id }
func apiNodeID(repoID, op string) string     { return "api:" + repoID + ":" + op }
func testNodeID(repoID, rel string) string   { return "test:" + repoID + ":" + rel }
func symbolNodeID(repoID, rel string) string { return "symbol:" + repoID + ":" + rel }
func deployNodeID(repoID, rel string) string { return "deploy:" + repoID + ":" + rel }
func knowledgeNodeID(id string) string       { return "knowledge:" + id }
func planTaskNodeID(kind, id string) string  { return kind + ":" + id }

// BuildFromWorkspace populates the impact graph from the workspace on disk. It
// first lays the structural spine from the workspace definition (IMPACT-004):
// one project node, one repository node per declared repository, and a
// `contains` edge from the project to each repository. It then runs the
// on-disk scanners (IMPACT-005 OpenAPI, IMPACT-006 tests, IMPACT-007 config,
// plus the source-symbol scanner) so a plain `analyze_impact` refresh
// populates the graph from the repositories' actual files. It reads the
// workspace through internal/workspace.Discover, so it works for both an
// explicit workspace.yaml and the implicit single-repo fallback. It is
// idempotent - every node/edge is upserted by a stable id, so calling it
// repeatedly (see Refresh) converges rather than duplicating.
//
// The injected-data adapters (BuildFromKnowledge / BuildFromPlanTasks) are
// deliberately NOT called here: their data lives in other packages and is
// supplied by the caller (the MCP server) to avoid an import cycle - see their
// doc comments.
func BuildFromWorkspace(root string) error {
	if err := buildStructuralSpine(root); err != nil {
		return err
	}
	// On-disk scanners are best-effort: they never fail the whole build on a
	// missing workspace or an unreadable/malformed file. A genuine write error
	// (disk failure) still propagates so a broken graph is not reported clean.
	for _, scan := range []func(string) error{
		BuildFromOpenAPI,
		BuildFromTests,
		BuildFromConfig,
		BuildFromSources,
	} {
		if err := scan(root); err != nil {
			return err
		}
	}
	return nil
}

// buildStructuralSpine lays the project/repository nodes and `contains` edges
// from the workspace definition (IMPACT-004).
func buildStructuralSpine(root string) error {
	ws, err := workspace.Discover(root)
	if err != nil {
		return fmt.Errorf("impact: discover workspace: %w", err)
	}

	projID := projectNodeID(ws.ID)
	projLabel := ws.Name
	if err := UpsertNode(root, protocol.ImpactNode{
		Id:    projID,
		Type:  protocol.ImpactNodeTypeProject,
		Label: &projLabel,
	}); err != nil {
		return err
	}

	for _, repo := range ws.Repositories {
		repoNodeID := repositoryNodeID(repo.ID)
		repoLabel := repo.ID
		repoAttr := repo.ID
		if err := UpsertNode(root, protocol.ImpactNode{
			Id:         repoNodeID,
			Type:       protocol.ImpactNodeTypeRepository,
			Label:      &repoLabel,
			Repository: &repoAttr,
		}); err != nil {
			return err
		}
		// The project contains each repository. This is an observed fact from
		// the workspace config, not an inference, so confidence is observed.
		method := "workspace-config"
		if err := UpsertEdge(root, protocol.ImpactEdge{
			From:         projID,
			To:           repoNodeID,
			Type:         protocol.ImpactEdgeTypeContains,
			Confidence:   protocol.ImpactEdgeConfidenceObserved,
			DiscoveredBy: &protocol.ImpactEdgeDiscoveredBy{Method: &method},
		}); err != nil {
			return err
		}
	}
	return nil
}

// Refresh re-runs the available builders to reconcile the graph with the
// current workspace (IMPACT-016). It is safe to call any time because every
// builder upserts by stable id, so a refresh converges the graph rather than
// duplicating it.
func Refresh(root string) error {
	return BuildFromWorkspace(root)
}

// --- on-disk scan helpers -------------------------------------------------

// forEachRepo resolves the workspace and invokes fn once per declared
// repository with its id and absolute path. A workspace that cannot be
// discovered is treated as "nothing to scan" (nil), not an error, so the
// best-effort scanners degrade gracefully when run outside a project.
func forEachRepo(root string, fn func(repoID, repoPath string) error) error {
	ws, err := workspace.Discover(root)
	if err != nil {
		return nil
	}
	for _, repo := range ws.Repositories {
		p, perr := ws.RepositoryPath(repo.ID)
		if perr != nil {
			continue
		}
		if err := fn(repo.ID, p); err != nil {
			return err
		}
	}
	return nil
}

// walkRepoFiles walks every regular file under repoPath (skipping VCS,
// dependency and punakawan-internal directories) and calls visit with the
// absolute path, the slash-normalized path relative to repoPath, and the
// dir entry. Unreadable entries and a missing repoPath are skipped silently -
// scanning is best-effort and must never fail the whole build on one bad path.
func walkRepoFiles(repoPath string, visit func(absPath, relPath string, d fs.DirEntry)) {
	_ = filepath.WalkDir(repoPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".punakawan", "node_modules", "vendor", "dist", "build", "target":
				if p != repoPath {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, rerr := filepath.Rel(repoPath, p)
		if rerr != nil {
			rel = p
		}
		visit(p, filepath.ToSlash(rel), d)
		return nil
	})
}

// upsertTypedNode is a thin convenience over UpsertNode for the scanners.
func upsertTypedNode(root, id string, typ protocol.ImpactNodeType, label, repo string, attrs map[string]interface{}) error {
	n := protocol.ImpactNode{Id: id, Type: typ}
	if label != "" {
		n.Label = &label
	}
	if repo != "" {
		n.Repository = &repo
	}
	if len(attrs) > 0 {
		n.Attributes = attrs
	}
	return UpsertNode(root, n)
}

// upsertEvidenceEdge is a thin convenience over UpsertEdge that attaches a
// single evidence ref and a discovery method, the shape every scanner emits.
func upsertEvidenceEdge(root, from, to string, typ protocol.ImpactEdgeType, conf protocol.ImpactEdgeConfidence, evType, evRef, method string) error {
	return UpsertEdge(root, protocol.ImpactEdge{
		From:         from,
		To:           to,
		Type:         typ,
		Confidence:   conf,
		Evidence:     []protocol.ImpactEdgeEvidenceElem{{Type: evType, Ref: &evRef}},
		DiscoveredBy: &protocol.ImpactEdgeDiscoveredBy{Method: &method},
	})
}

// --- IMPACT-005: OpenAPI builder ------------------------------------------

var httpMethods = map[string]bool{
	"get": true, "post": true, "put": true, "delete": true,
	"patch": true, "options": true, "head": true, "trace": true,
}

// openapiSpec is a permissive view of the only field the builder needs: the
// paths map. Operation objects are left as interface{} so non-method keys at
// the path level (e.g. a "parameters" sequence) never break decoding.
type openapiSpec struct {
	Paths map[string]map[string]interface{} `yaml:"paths" json:"paths"`
}

// isOpenAPISpecName reports whether a filename looks like an OpenAPI/Swagger
// spec (name contains openapi/swagger and has a yaml/yml/json extension).
func isOpenAPISpecName(lowerName string) bool {
	if !(strings.Contains(lowerName, "openapi") || strings.Contains(lowerName, "swagger")) {
		return false
	}
	return strings.HasSuffix(lowerName, ".yaml") ||
		strings.HasSuffix(lowerName, ".yml") ||
		strings.HasSuffix(lowerName, ".json")
}

// BuildFromOpenAPI scans each repository for OpenAPI/Swagger specs
// (*openapi*.{yaml,yml,json}, *swagger*.{...}) and, per path+method, adds an
// api_operation node plus a `documented_by` edge from the repository (the
// service that publishes the spec) to that operation. The operation id, when
// present, keys the node so it is stable across path renames; otherwise the
// "METHOD path" pair keys it. Malformed specs are skipped, never fatal.
func BuildFromOpenAPI(root string) error {
	return forEachRepo(root, func(repoID, repoPath string) error {
		var ferr error
		walkRepoFiles(repoPath, func(abs, rel string, d fs.DirEntry) {
			if ferr != nil {
				return
			}
			if !isOpenAPISpecName(strings.ToLower(d.Name())) {
				return
			}
			ferr = indexOpenAPISpec(root, repoID, abs, rel)
		})
		return ferr
	})
}

// indexOpenAPISpec parses one spec file and upserts its operations. A parse or
// read failure returns nil (skip this file); only a graph write error is fatal.
func indexOpenAPISpec(root, repoID, absPath, relPath string) error {
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil
	}
	var spec openapiSpec
	if strings.HasSuffix(strings.ToLower(absPath), ".json") {
		if json.Unmarshal(raw, &spec) != nil {
			return nil
		}
	} else if yaml.Unmarshal(raw, &spec) != nil {
		return nil
	}

	repoNode := repositoryNodeID(repoID)
	for path, methods := range spec.Paths {
		for method, opRaw := range methods {
			m := strings.ToLower(method)
			if !httpMethods[m] {
				continue
			}
			opID := ""
			if opMap, ok := opRaw.(map[string]interface{}); ok {
				opID, _ = opMap["operationId"].(string)
			}
			opKey := opID
			if opKey == "" {
				opKey = strings.ToUpper(m) + " " + path
			}
			nodeID := apiNodeID(repoID, opKey)
			label := strings.ToUpper(m) + " " + path
			attrs := map[string]interface{}{"path": path, "method": strings.ToUpper(m), "spec": relPath}
			if opID != "" {
				attrs["operationId"] = opID
			}
			if err := upsertTypedNode(root, nodeID, protocol.ImpactNodeTypeApiOperation, label, repoID, attrs); err != nil {
				return err
			}
			// The service repository documents (publishes) this operation via
			// its spec. Observed: the operation is literally present in the file.
			if err := upsertEvidenceEdge(root, repoNode, nodeID,
				protocol.ImpactEdgeTypeDocumentedBy, protocol.ImpactEdgeConfidenceObserved,
				"openapi_reference", relPath+"#"+label, "openapi-spec"); err != nil {
				return err
			}
		}
	}
	return nil
}

// --- IMPACT-006: Test builder ---------------------------------------------

// sourceExtsByLang lists the source-file extensions considered a sibling of a
// test file of the given language.
var sourceExtsByLang = map[string][]string{
	"go":     {".go"},
	"java":   {".java"},
	"kotlin": {".kt"},
	"python": {".py"},
	"js":     {".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"},
}

// testLanguage classifies a filename as a test file and returns its language.
func testLanguage(name string) (string, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "_test.go"):
		return "go", true
	case strings.HasSuffix(name, "Test.java") || strings.HasSuffix(name, "Tests.java"):
		return "java", true
	case strings.HasSuffix(name, "Test.kt") || strings.HasSuffix(name, "Tests.kt"):
		return "kotlin", true
	case strings.HasSuffix(lower, "_test.py") || (strings.HasPrefix(lower, "test_") && strings.HasSuffix(lower, ".py")):
		return "python", true
	case (strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec.")) && hasAnyExt(lower, sourceExtsByLang["js"]):
		return "js", true
	}
	return "", false
}

// hasAnyExt reports whether name ends with any of exts.
func hasAnyExt(name string, exts []string) bool {
	for _, e := range exts {
		if strings.HasSuffix(name, e) {
			return true
		}
	}
	return false
}

// BuildFromTests scans each repository for test files (Go *_test.go, JS/TS
// *.test.* / *.spec.*, Python test_*.py / *_test.py, Java/Kotlin *Test*.java /
// *Test*.kt) and adds a test node per file. It then attaches `tests` edges to
// the source files it most likely exercises.
//
// LIMITATION: association is a same-directory heuristic, not import/AST
// analysis - a test node is linked to sibling non-test source files of the
// same language in its own directory. This over- or under-links in projects
// that separate tests from sources; a later AST-based pass (IMPACT source
// symbol extraction) can upgrade these `inferred` edges to `verified`.
func BuildFromTests(root string) error {
	return forEachRepo(root, func(repoID, repoPath string) error {
		var ferr error
		walkRepoFiles(repoPath, func(abs, rel string, d fs.DirEntry) {
			if ferr != nil {
				return
			}
			lang, ok := testLanguage(d.Name())
			if !ok {
				return
			}
			ferr = indexTestFile(root, repoID, repoPath, abs, rel, lang)
		})
		return ferr
	})
}

// indexTestFile upserts one test node and the `tests` edges to its sibling
// source files (same-directory heuristic).
func indexTestFile(root, repoID, repoPath, absPath, relPath, lang string) error {
	testID := testNodeID(repoID, relPath)
	if err := upsertTypedNode(root, testID, protocol.ImpactNodeTypeTest, filepath.Base(relPath), repoID,
		map[string]interface{}{"path": relPath, "language": lang}); err != nil {
		return err
	}

	exts := sourceExtsByLang[lang]
	entries, err := os.ReadDir(filepath.Dir(absPath))
	if err != nil {
		return nil // cannot list siblings; the test node alone is still useful
	}
	dirRel := filepath.Dir(relPath)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if _, isTest := testLanguage(name); isTest {
			continue
		}
		if !hasAnyExt(strings.ToLower(name), exts) {
			continue
		}
		sibRel := filepath.ToSlash(filepath.Join(dirRel, name))
		symID := symbolNodeID(repoID, sibRel)
		if err := upsertTypedNode(root, symID, protocol.ImpactNodeTypeSourceSymbol, name, repoID,
			map[string]interface{}{"path": sibRel, "language": lang}); err != nil {
			return err
		}
		// inferred: the link is a directory heuristic, not a proven reference.
		if err := upsertEvidenceEdge(root, testID, symID,
			protocol.ImpactEdgeTypeTests, protocol.ImpactEdgeConfidenceInferred,
			"test_reference", relPath, "same-directory-heuristic"); err != nil {
			return err
		}
	}
	return nil
}

// --- IMPACT-007: Configuration / deployment builder ------------------------

// classifyConfig reports the kind of config/deploy descriptor a file is, or ""
// if it is not one. needsContent is true when the classification could only be
// decided by inspecting the file body (generic YAML that may be a k8s manifest).
func classifyConfig(lowerName, relPath string) (kind string, needsContent bool) {
	slashRel := filepath.ToSlash(relPath)
	switch {
	case lowerName == "dockerfile" || strings.HasPrefix(lowerName, "dockerfile.") || strings.HasSuffix(lowerName, ".dockerfile"):
		return "dockerfile", false
	case strings.HasSuffix(lowerName, ".tf"):
		return "terraform", false
	case strings.HasPrefix(lowerName, "docker-compose") && (strings.HasSuffix(lowerName, ".yml") || strings.HasSuffix(lowerName, ".yaml")):
		return "docker-compose", false
	case lowerName == ".gitlab-ci.yml" || lowerName == ".gitlab-ci.yaml" || strings.HasSuffix(lowerName, ".gitlab-ci.yml"):
		return "ci", false
	case strings.Contains(slashRel, ".github/workflows/") && (strings.HasSuffix(lowerName, ".yml") || strings.HasSuffix(lowerName, ".yaml")):
		return "ci", false
	case strings.HasSuffix(lowerName, ".yml") || strings.HasSuffix(lowerName, ".yaml"):
		// Possibly a Kubernetes manifest - decide from the body.
		return "kubernetes", true
	}
	return "", false
}

// looksLikeK8s heuristically detects a Kubernetes manifest: it has both a
// top-level apiVersion and kind. Cheap substring check, best-effort.
func looksLikeK8s(content string) bool {
	return (strings.Contains(content, "\napiVersion:") || strings.HasPrefix(content, "apiVersion:")) &&
		(strings.Contains(content, "\nkind:") || strings.HasPrefix(content, "kind:"))
}

// BuildFromConfig scans each repository for config/deploy descriptors
// (Dockerfile, *.tf, docker-compose*, Kubernetes manifests with kind:, and CI
// workflow files) and adds a deployment_artifact node per file plus a
// `configures` edge from the artifact to the repository it configures.
func BuildFromConfig(root string) error {
	return forEachRepo(root, func(repoID, repoPath string) error {
		var ferr error
		walkRepoFiles(repoPath, func(abs, rel string, d fs.DirEntry) {
			if ferr != nil {
				return
			}
			kind, needsContent := classifyConfig(strings.ToLower(d.Name()), rel)
			if kind == "" {
				return
			}
			if needsContent {
				raw, err := os.ReadFile(abs)
				if err != nil || !looksLikeK8s(string(raw)) {
					return
				}
			}
			ferr = indexConfigFile(root, repoID, rel, kind)
		})
		return ferr
	})
}

// indexConfigFile upserts one deployment_artifact node and its `configures`
// edge to the repository.
func indexConfigFile(root, repoID, relPath, kind string) error {
	nodeID := deployNodeID(repoID, relPath)
	if err := upsertTypedNode(root, nodeID, protocol.ImpactNodeTypeDeploymentArtifact, filepath.Base(relPath), repoID,
		map[string]interface{}{"path": relPath, "kind": kind}); err != nil {
		return err
	}
	// observed: the descriptor is literally present and names/targets this repo.
	return upsertEvidenceEdge(root, nodeID, repositoryNodeID(repoID),
		protocol.ImpactEdgeTypeConfigures, protocol.ImpactEdgeConfidenceObserved,
		"config_file", relPath, "config-scan")
}

// --- injected-data adapters (no cross-package imports) --------------------

// KnowledgeRef is the minimal, self-contained view of a knowledge record the
// impact graph needs. It is defined locally (not imported from the knowledge
// package) so BuildFromKnowledge takes plain data by dependency injection and
// this package never imports the knowledge subsystem - that would create an
// import cycle (the knowledge/MCP layers already depend on impact). The caller
// (mcpserver) maps its own records into this shape.
type KnowledgeRef struct {
	// ID is the knowledge record's stable identifier.
	ID string
	// Title is a human-readable label for the record.
	Title string
	// Repository is the owning repository id, when applicable (may be empty).
	Repository string
	// Relates lists impact node ids this record relates to (e.g. the symbols,
	// operations or repositories it documents). Each becomes a `derived_from`
	// edge from the related node to this knowledge_record node.
	Relates []string
}

// BuildFromKnowledge (IMPACT-008) upserts a knowledge_record node per record
// and a `derived_from` edge from each related node to the record. It takes the
// records as injected data rather than reading the knowledge store directly,
// to avoid an import cycle - see KnowledgeRef. It is idempotent by node id.
func BuildFromKnowledge(root string, records []KnowledgeRef) error {
	for _, rec := range records {
		if rec.ID == "" {
			continue
		}
		nodeID := knowledgeNodeID(rec.ID)
		if err := upsertTypedNode(root, nodeID, protocol.ImpactNodeTypeKnowledgeRecord, rec.Title, rec.Repository, nil); err != nil {
			return err
		}
		for _, target := range rec.Relates {
			if target == "" {
				continue
			}
			if err := upsertEvidenceEdge(root, target, nodeID,
				protocol.ImpactEdgeTypeDerivedFrom, protocol.ImpactEdgeConfidenceInferred,
				"knowledge_reference", rec.ID, "knowledge-adapter"); err != nil {
				return err
			}
		}
	}
	return nil
}

// PlanTaskRef is the minimal, self-contained view of a workflow, plan or task
// the impact graph needs. Like KnowledgeRef it is defined locally so the
// workflow/plan/task adapters (IMPACT-009) receive plain injected data and this
// package never imports the workflow/task subsystems (avoiding an import
// cycle). The caller (mcpserver) maps its own records into this shape.
type PlanTaskRef struct {
	// ID is the workflow/plan/task stable identifier.
	ID string
	// Kind is "workflow", "plan" or "task"; it selects the node type and id
	// prefix. Any other value defaults to a task node.
	Kind string
	// Title is a human-readable label.
	Title string
	// Repository is the owning repository id, when applicable (may be empty).
	Repository string
	// ParentID is the impact node id of the parent (e.g. the plan a task
	// belongs to, or the workflow a plan belongs to). When set, a `contains`
	// edge is emitted from the parent to this node.
	ParentID string
	// Tracks lists impact node ids this workflow/plan/task tracks or affects.
	// Each becomes a `tracked_by` edge from the tracked node to this node.
	Tracks []string
}

// planTaskNodeType maps a PlanTaskRef.Kind to a node type, defaulting to task.
func planTaskNodeType(kind string) (protocol.ImpactNodeType, string) {
	switch kind {
	case "workflow":
		return protocol.ImpactNodeTypeWorkflow, "workflow"
	case "plan":
		return protocol.ImpactNodeTypePlan, "plan"
	default:
		return protocol.ImpactNodeTypeTask, "task"
	}
}

// BuildFromPlanTasks (IMPACT-009) upserts a workflow/plan/task node per ref, a
// `contains` edge from its parent when set, and a `tracked_by` edge from each
// tracked node to this node. It takes the refs as injected data rather than
// reading the workflow/task stores directly, to avoid an import cycle - see
// PlanTaskRef. It is idempotent by node id.
func BuildFromPlanTasks(root string, refs []PlanTaskRef) error {
	for _, ref := range refs {
		if ref.ID == "" {
			continue
		}
		typ, prefix := planTaskNodeType(ref.Kind)
		nodeID := planTaskNodeID(prefix, ref.ID)
		if err := upsertTypedNode(root, nodeID, typ, ref.Title, ref.Repository, nil); err != nil {
			return err
		}
		if ref.ParentID != "" {
			if err := upsertEvidenceEdge(root, ref.ParentID, nodeID,
				protocol.ImpactEdgeTypeContains, protocol.ImpactEdgeConfidenceObserved,
				"plan_hierarchy", ref.ID, "task-adapter"); err != nil {
				return err
			}
		}
		for _, target := range ref.Tracks {
			if target == "" {
				continue
			}
			if err := upsertEvidenceEdge(root, target, nodeID,
				protocol.ImpactEdgeTypeTrackedBy, protocol.ImpactEdgeConfidenceObserved,
				"task_reference", ref.ID, "task-adapter"); err != nil {
				return err
			}
		}
	}
	return nil
}

// BuildFromSources will add source_symbol nodes and calls/defines edges from
// each repository's source code via AST analysis. Left as a no-op for now;
// BuildFromTests already emits best-effort source_symbol nodes for files a
// test sits beside, which a real AST pass can later enrich and upgrade.
// TODO(IMPACT source symbols): implement AST-based source symbol extraction.
func BuildFromSources(root string) error { return nil }

// BuildFromDeploy is retained as a no-op: deployment descriptors are handled by
// BuildFromConfig, which emits deployment_artifact nodes for Dockerfiles, k8s
// manifests, compose files, Terraform and CI workflows.
// TODO(IMPACT deploy): fold any remaining deploy-only manifests in here.
func BuildFromDeploy(root string) error { return nil }
