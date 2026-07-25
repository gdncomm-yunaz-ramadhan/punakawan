package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CreateChangeDossierInput is create_change_dossier's input.
type CreateChangeDossierInput struct {
	Id        string                          `json:"id,omitempty" jsonschema:"optional stable id; the server mints one when omitted"`
	Title     string                          `json:"title" jsonschema:"human-readable title of the change"`
	Objective protocol.ChangeDossierObjective `json:"objective" jsonschema:"the change objective: a statement and its source refs"`
}

// ChangeDossierOutput carries a full change dossier for the tools that return one.
type ChangeDossierOutput struct {
	Dossier protocol.ChangeDossier `json:"dossier"`
}

func createChangeDossierHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreateChangeDossierInput) (*mcp.CallToolResult, ChangeDossierOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateChangeDossierInput) (*mcp.CallToolResult, ChangeDossierOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Semar, "change_dossier"); err != nil {
			return nil, ChangeDossierOutput{}, err
		}
		id := in.Id
		if id == "" {
			id = randomLocalID("dossier")
		}
		d, err := dossier.Create(a.Workspace.Root, protocol.ChangeDossier{
			Id:        id,
			ProjectId: a.Workspace.ID,
			Title:     in.Title,
			Objective: in.Objective,
		})
		if err != nil {
			return nil, ChangeDossierOutput{}, fmt.Errorf("mcpserver: create change dossier: %w", err)
		}
		return nil, ChangeDossierOutput{Dossier: d}, nil
	}
}

// DossierClaimInput is the claim payload for add_dossier_claim. It is a
// dedicated type (not protocol.DossierClaim) so id and status stay optional at
// the MCP boundary - the server mints the id and AddClaim defaults the status
// to claimed - rather than being schema-required.
type DossierClaimInput struct {
	Id        string                        `json:"id,omitempty" jsonschema:"optional stable id; the server mints one when omitted"`
	Producer  protocol.DossierClaimProducer `json:"producer" jsonschema:"the role that produced the claim"`
	Type      string                        `json:"type" jsonschema:"e.g. compatibility, implementation, risk, completeness"`
	Statement string                        `json:"statement" jsonschema:"the claim being asserted"`
	Status    protocol.DossierClaimStatus   `json:"status,omitempty" jsonschema:"optional; defaults to claimed"`
	Evidence  []string                      `json:"evidence,omitempty" jsonschema:"supporting evidence record ids"`
}

// AddDossierClaimInput is add_dossier_claim's input.
type AddDossierClaimInput struct {
	DossierId string            `json:"dossier_id" jsonschema:"the dossier this claim belongs to"`
	Claim     DossierClaimInput `json:"claim" jsonschema:"the claim: producer role, type, statement, optional evidence ids; status defaults to claimed"`
}

// DossierClaimOutput carries a single claim.
type DossierClaimOutput struct {
	Claim protocol.DossierClaim `json:"claim"`
}

func addDossierClaimHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, AddDossierClaimInput) (*mcp.CallToolResult, DossierClaimOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in AddDossierClaimInput) (*mcp.CallToolResult, DossierClaimOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Semar, "change_dossier"); err != nil {
			return nil, DossierClaimOutput{}, err
		}
		id := in.Claim.Id
		if id == "" {
			id = randomLocalID("claim")
		}
		claim := protocol.DossierClaim{
			Id:        id,
			Producer:  in.Claim.Producer,
			Type:      in.Claim.Type,
			Statement: in.Claim.Statement,
			Status:    in.Claim.Status,
			Evidence:  in.Claim.Evidence,
		}
		stored, err := dossier.AddClaim(a.Workspace.Root, in.DossierId, claim)
		if err != nil {
			return nil, DossierClaimOutput{}, fmt.Errorf("mcpserver: add dossier claim: %w", err)
		}
		return nil, DossierClaimOutput{Claim: stored}, nil
	}
}

// VerifyDossierClaimInput is verify_dossier_claim / dispute_dossier_claim's input.
type VerifyDossierClaimInput struct {
	DossierId string `json:"dossier_id" jsonschema:"the dossier the claim belongs to"`
	ClaimId   string `json:"claim_id" jsonschema:"the claim to verify or dispute"`
	ByRole    string `json:"by_role" jsonschema:"the verifying role; must differ from the claim's producer (§34)"`
	Note      string `json:"note,omitempty" jsonschema:"optional verification note"`
}

// selfVerificationError rewraps ErrSelfVerification into a clear, agent-facing
// MCP error: a role cannot verify or dispute its own claim (§34).
func selfVerificationError(err error, claimID, byRole string) error {
	if errors.Is(err, dossier.ErrSelfVerification) {
		return fmt.Errorf("mcpserver: role %q cannot verify or dispute claim %q it produced itself; an independent role must check it (§34)", byRole, claimID)
	}
	return err
}

func verifyDossierClaimHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, VerifyDossierClaimInput) (*mcp.CallToolResult, DossierClaimOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in VerifyDossierClaimInput) (*mcp.CallToolResult, DossierClaimOutput, error) {
		stored, err := dossier.VerifyClaim(a.Workspace.Root, in.DossierId, in.ClaimId, in.ByRole, in.Note)
		if err != nil {
			return nil, DossierClaimOutput{}, selfVerificationError(err, in.ClaimId, in.ByRole)
		}
		return nil, DossierClaimOutput{Claim: stored}, nil
	}
}

func disputeDossierClaimHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, VerifyDossierClaimInput) (*mcp.CallToolResult, DossierClaimOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in VerifyDossierClaimInput) (*mcp.CallToolResult, DossierClaimOutput, error) {
		stored, err := dossier.DisputeClaim(a.Workspace.Root, in.DossierId, in.ClaimId, in.ByRole, in.Note)
		if err != nil {
			return nil, DossierClaimOutput{}, selfVerificationError(err, in.ClaimId, in.ByRole)
		}
		return nil, DossierClaimOutput{Claim: stored}, nil
	}
}

// DossierEvidenceInput is the evidence payload for add_dossier_evidence. Like
// DossierClaimInput it keeps id optional (the server mints one) rather than
// schema-required.
type DossierEvidenceInput struct {
	Id        string                                  `json:"id,omitempty" jsonschema:"optional stable id; the server mints one when omitted"`
	Type      protocol.DossierEvidenceType            `json:"type" jsonschema:"e.g. test_result, diff, api_compatibility, review_result, manual_confirmation"`
	Artifacts []protocol.DossierEvidenceArtifactsElem `json:"artifacts,omitempty" jsonschema:"artifact paths and caller-supplied sha256 hashes"`
	Source    *protocol.DossierEvidenceSource         `json:"source,omitempty"`
	Result    *protocol.DossierEvidenceResult         `json:"result,omitempty"`
}

// AddDossierEvidenceInput is add_dossier_evidence's input.
type AddDossierEvidenceInput struct {
	DossierId string               `json:"dossier_id" jsonschema:"the dossier this evidence belongs to"`
	Evidence  DossierEvidenceInput `json:"evidence" jsonschema:"the evidence record: type, optional artifacts, source, and result"`
}

// DossierEvidenceOutput carries a single evidence record.
type DossierEvidenceOutput struct {
	Evidence protocol.DossierEvidence `json:"evidence"`
}

func addDossierEvidenceHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, AddDossierEvidenceInput) (*mcp.CallToolResult, DossierEvidenceOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in AddDossierEvidenceInput) (*mcp.CallToolResult, DossierEvidenceOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Semar, "change_dossier"); err != nil {
			return nil, DossierEvidenceOutput{}, err
		}
		id := in.Evidence.Id
		if id == "" {
			id = randomLocalID("evidence")
		}
		ev := protocol.DossierEvidence{
			Id:        id,
			Type:      in.Evidence.Type,
			Artifacts: in.Evidence.Artifacts,
			Source:    in.Evidence.Source,
			Result:    in.Evidence.Result,
		}
		stored, err := dossier.AddEvidence(a.Workspace.Root, in.DossierId, ev)
		if err != nil {
			return nil, DossierEvidenceOutput{}, fmt.Errorf("mcpserver: add dossier evidence: %w", err)
		}
		return nil, DossierEvidenceOutput{Evidence: stored}, nil
	}
}

// SetDossierContradictionsInput is set_dossier_contradictions's input. The
// unresolved set drives Finalize blocking (§34): a dossier with unresolved
// contradictions cannot be finalized.
type SetDossierContradictionsInput struct {
	DossierId  string   `json:"dossier_id" jsonschema:"the dossier to update"`
	Resolved   []string `json:"resolved,omitempty" jsonschema:"resolved contradiction ids"`
	Unresolved []string `json:"unresolved,omitempty" jsonschema:"unresolved contradiction ids; these block finalization"`
}

func setDossierContradictionsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SetDossierContradictionsInput) (*mcp.CallToolResult, ChangeDossierOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SetDossierContradictionsInput) (*mcp.CallToolResult, ChangeDossierOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Semar, "change_dossier"); err != nil {
			return nil, ChangeDossierOutput{}, err
		}
		d, err := dossier.SetContradictions(a.Workspace.Root, in.DossierId, in.Resolved, in.Unresolved, dossier.PutOptions{})
		if err != nil {
			return nil, ChangeDossierOutput{}, fmt.Errorf("mcpserver: set dossier contradictions: %w", err)
		}
		return nil, ChangeDossierOutput{Dossier: d}, nil
	}
}

// DossierExcludedRepoInput mirrors dossier.ExcludedRepository at the MCP
// boundary: a repository deliberately left out of the change, with the reason
// (§33 impact section).
type DossierExcludedRepoInput struct {
	Repository string `json:"repository" jsonschema:"the excluded repository"`
	Reason     string `json:"reason" jsonschema:"why it is excluded from this change"`
}

// SetDossierImpactInput is set_dossier_impact's input.
type SetDossierImpactInput struct {
	DossierId            string                     `json:"dossier_id" jsonschema:"the dossier to update"`
	Repositories         []string                   `json:"repositories,omitempty" jsonschema:"repositories this change affects"`
	ExcludedRepositories []DossierExcludedRepoInput `json:"excluded_repositories,omitempty" jsonschema:"repositories deliberately excluded, each with a reason"`
	MissingCoverage      []string                   `json:"missing_coverage,omitempty" jsonschema:"impacted areas with no test/verification coverage"`
}

func setDossierImpactHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SetDossierImpactInput) (*mcp.CallToolResult, ChangeDossierOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SetDossierImpactInput) (*mcp.CallToolResult, ChangeDossierOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Semar, "change_dossier"); err != nil {
			return nil, ChangeDossierOutput{}, err
		}
		excluded := make([]dossier.ExcludedRepository, 0, len(in.ExcludedRepositories))
		for _, e := range in.ExcludedRepositories {
			excluded = append(excluded, dossier.ExcludedRepository{Repository: e.Repository, Reason: e.Reason})
		}
		d, err := dossier.SetImpact(a.Workspace.Root, in.DossierId, dossier.ImpactSection{
			Repositories:         in.Repositories,
			ExcludedRepositories: excluded,
			MissingCoverage:      in.MissingCoverage,
		}, dossier.PutOptions{})
		if err != nil {
			return nil, ChangeDossierOutput{}, fmt.Errorf("mcpserver: set dossier impact: %w", err)
		}
		return nil, ChangeDossierOutput{Dossier: d}, nil
	}
}

// FinalizeDossierInput is finalize_dossier's input.
type FinalizeDossierInput struct {
	DossierId string `json:"dossier_id" jsonschema:"the dossier to finalize (must be at verified status and free of blocking findings)"`
}

func finalizeDossierHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, FinalizeDossierInput) (*mcp.CallToolResult, ChangeDossierOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in FinalizeDossierInput) (*mcp.CallToolResult, ChangeDossierOutput, error) {
		if err := authorizeRoleSubmit(a, roleconfig.Semar, "change_dossier"); err != nil {
			return nil, ChangeDossierOutput{}, err
		}
		root := a.Workspace.Root
		if err := dossier.Finalize(root, in.DossierId); err != nil {
			var be *dossier.BlockingError
			if errors.As(err, &be) {
				return nil, ChangeDossierOutput{}, fmt.Errorf("mcpserver: dossier %q cannot be finalized - %d blocking finding(s): %s", in.DossierId, len(be.Blockers), strings.Join(be.Blockers, "; "))
			}
			return nil, ChangeDossierOutput{}, fmt.Errorf("mcpserver: finalize dossier: %w", err)
		}
		loaded, err := dossier.Get(root, in.DossierId)
		if err != nil {
			return nil, ChangeDossierOutput{}, fmt.Errorf("mcpserver: read finalized dossier: %w", err)
		}
		return nil, ChangeDossierOutput{Dossier: loaded.Dossier}, nil
	}
}

// ExportDossierInput is export_dossier's input.
type ExportDossierInput struct {
	DossierId string `json:"dossier_id" jsonschema:"the dossier to export"`
	Format    string `json:"format,omitempty" jsonschema:"md (default) or json"`
}

// ExportDossierOutput carries the rendered export.
type ExportDossierOutput struct {
	Format  string `json:"format"`
	Content string `json:"content"`
}

func exportDossierHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ExportDossierInput) (*mcp.CallToolResult, ExportDossierOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ExportDossierInput) (*mcp.CallToolResult, ExportDossierOutput, error) {
		root := a.Workspace.Root
		format := in.Format
		if format == "" {
			format = "md"
		}
		switch format {
		case "md":
			md, err := dossier.ExportMarkdown(root, in.DossierId)
			if err != nil {
				return nil, ExportDossierOutput{}, fmt.Errorf("mcpserver: export dossier markdown: %w", err)
			}
			return nil, ExportDossierOutput{Format: format, Content: md}, nil
		case "json":
			raw, err := dossier.ExportJSON(root, in.DossierId)
			if err != nil {
				return nil, ExportDossierOutput{}, fmt.Errorf("mcpserver: export dossier json: %w", err)
			}
			return nil, ExportDossierOutput{Format: format, Content: string(raw)}, nil
		default:
			return nil, ExportDossierOutput{}, fmt.Errorf("mcpserver: export dossier: unknown format %q (want md or json)", format)
		}
	}
}
