package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/impact"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// defaultImpactDepth is the traversal depth analyze_impact uses when the caller
// does not specify one (§29).
const defaultImpactDepth = 3

// AnalyzeImpactInput is analyze_impact's input.
type AnalyzeImpactInput struct {
	SubjectId string   `json:"subject_id" jsonschema:"the impact-graph node id to analyze from, e.g. api:affiliate-api:getMerchantBadge"`
	Depth     int      `json:"depth,omitempty" jsonschema:"traversal depth in hops (default 3)"`
	Include   []string `json:"include,omitempty" jsonschema:"restrict transitive impact to these node types"`
	Refresh   bool     `json:"refresh,omitempty" jsonschema:"reconcile the structural graph from the workspace before querying"`
}

// AnalyzeImpactOutput is analyze_impact's output. It mirrors impact.ImpactResult
// but with json-tagged, omitempty slices so an empty impact set serializes as an
// omitted field rather than a null (the MCP output schema rejects null arrays).
type AnalyzeImpactOutput struct {
	DirectImpact          []protocol.ImpactNode `json:"direct_impact,omitempty"`
	TransitiveImpact      []protocol.ImpactNode `json:"transitive_impact,omitempty"`
	AffectedRepositories  []string              `json:"affected_repositories,omitempty"`
	AffectedTests         []protocol.ImpactNode `json:"affected_tests,omitempty"`
	DeploymentArtifacts   []protocol.ImpactNode `json:"deployment_artifacts,omitempty"`
	Owners                []protocol.ImpactNode `json:"owners,omitempty"`
	MissingCoverage       []protocol.ImpactNode `json:"missing_coverage,omitempty"`
	RelatedContradictions []string              `json:"related_contradictions,omitempty"`
}

func analyzeImpactHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, AnalyzeImpactInput) (*mcp.CallToolResult, AnalyzeImpactOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in AnalyzeImpactInput) (*mcp.CallToolResult, AnalyzeImpactOutput, error) {
		root := a.Workspace.Root
		if in.Refresh {
			if err := impact.Refresh(root); err != nil {
				return nil, AnalyzeImpactOutput{}, fmt.Errorf("mcpserver: refresh impact graph: %w", err)
			}
		}
		depth := in.Depth
		if depth <= 0 {
			depth = defaultImpactDepth
		}
		res, err := impact.Query(root, in.SubjectId, depth, in.Include)
		if err != nil {
			return nil, AnalyzeImpactOutput{}, fmt.Errorf("mcpserver: query impact: %w", err)
		}
		return nil, AnalyzeImpactOutput{
			DirectImpact:          res.DirectImpact,
			TransitiveImpact:      res.TransitiveImpact,
			AffectedRepositories:  res.AffectedRepositories,
			AffectedTests:         res.AffectedTests,
			DeploymentArtifacts:   res.DeploymentArtifacts,
			Owners:                res.Owners,
			MissingCoverage:       res.MissingCoverage,
			RelatedContradictions: res.RelatedContradictions,
		}, nil
	}
}

// RecordImpactEdgeInput is record_impact_edge's input.
type RecordImpactEdgeInput struct {
	From         string                            `json:"from" jsonschema:"source node id"`
	To           string                            `json:"to" jsonschema:"target node id"`
	Type         protocol.ImpactEdgeType           `json:"type" jsonschema:"edge type, e.g. calls|consumes|tests|depends_on|contradicts"`
	Confidence   protocol.ImpactEdgeConfidence     `json:"confidence" jsonschema:"observed|inferred|verified|disputed"`
	Evidence     []protocol.ImpactEdgeEvidenceElem `json:"evidence,omitempty" jsonschema:"supporting references for the edge"`
	DiscoveredBy string                            `json:"discovered_by,omitempty" jsonschema:"role that discovered this dependency"`
}

// RecordImpactEdgeOutput is record_impact_edge's output.
type RecordImpactEdgeOutput struct {
	Edge protocol.ImpactEdge `json:"edge"`
}

func recordImpactEdgeHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RecordImpactEdgeInput) (*mcp.CallToolResult, RecordImpactEdgeOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RecordImpactEdgeInput) (*mcp.CallToolResult, RecordImpactEdgeOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Gareng, "cross_repository_impact"); err != nil {
			return nil, RecordImpactEdgeOutput{}, err
		}
		edge := protocol.ImpactEdge{
			From:       in.From,
			To:         in.To,
			Type:       in.Type,
			Confidence: in.Confidence,
			Evidence:   in.Evidence,
		}
		if in.DiscoveredBy != "" {
			role := in.DiscoveredBy
			edge.DiscoveredBy = &protocol.ImpactEdgeDiscoveredBy{Role: &role}
		}
		if err := impact.UpsertEdge(a.Workspace.Root, edge); err != nil {
			return nil, RecordImpactEdgeOutput{}, fmt.Errorf("mcpserver: record impact edge: %w", err)
		}
		return nil, RecordImpactEdgeOutput{Edge: edge}, nil
	}
}
