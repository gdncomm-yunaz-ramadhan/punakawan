package impact

import (
	"fmt"

	"github.com/ygrip/punakawan/internal/workspace"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Builder id conventions. Ids are stable and typed so every builder can upsert
// idempotently (§24): re-running a build re-emits the same ids and the JSONL
// fold keeps the graph converged rather than growing duplicates.
func projectNodeID(id string) string    { return "project:" + id }
func repositoryNodeID(id string) string { return "repository:" + id }

// BuildFromWorkspace populates the impact graph's structural spine from the
// workspace definition (IMPACT-004): one project node, one repository node per
// declared repository, and a `contains` edge from the project to each
// repository. It reads the workspace through internal/workspace.Discover, so it
// works for both an explicit workspace.yaml and the implicit single-repo
// fallback. It is idempotent - every node/edge is upserted by a stable id, so
// calling it repeatedly (see Refresh) converges rather than duplicating.
func BuildFromWorkspace(root string) error {
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
// duplicating it. As the stubbed builders below are implemented they should be
// added here.
func Refresh(root string) error {
	return BuildFromWorkspace(root)
}

// The builders below are intentionally stubs that return nil without writing
// anything: producing fake nodes/edges would poison impact queries with data
// that was never observed. Each is tracked by its own bd issue and will read a
// real source (an OpenAPI spec, a test suite, config files, deploy manifests,
// source symbols) and upsert the corresponding nodes and edges. They are kept
// as named no-ops so callers (and Refresh) can wire them in ahead of their
// implementations without a build break.

// BuildFromOpenAPI will add api_operation nodes and consumes/documented_by
// edges from each repository's OpenAPI specs.
// TODO(IMPACT-005): implement OpenAPI operation extraction.
func BuildFromOpenAPI(root string) error { return nil }

// BuildFromTests will add test nodes and `tests` edges by parsing each
// repository's test suites.
// TODO(IMPACT-006): implement test-to-symbol/operation extraction.
func BuildFromTests(root string) error { return nil }

// BuildFromConfig will add configuration_key nodes and `configures` edges from
// each repository's config files.
// TODO(IMPACT-007): implement configuration key extraction.
func BuildFromConfig(root string) error { return nil }

// BuildFromDeploy will add deployment_artifact nodes and `deploys` edges from
// each repository's deployment manifests.
// TODO(IMPACT-008): implement deployment artifact extraction.
func BuildFromDeploy(root string) error { return nil }

// BuildFromSources will add source_symbol nodes and calls/defines edges from
// each repository's source code.
// TODO(IMPACT-009): implement source symbol extraction.
func BuildFromSources(root string) error { return nil }
