package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// BuildContextDossierInput is build_context_dossier's input. The workspace
// id is not a parameter: the server already knows which workspace it was
// started against.
type BuildContextDossierInput struct {
	RunId                string   `json:"run_id" jsonschema:"the workflow run this dossier belongs to"`
	AffectedRepositories []string `json:"affected_repositories,omitempty" jsonschema:"repository ids believed relevant; defaults to every repository in the workspace if omitted"`
	UserGoal             string   `json:"user_goal,omitempty" jsonschema:"the user's stated goal, in their own words"`
	BusinessOrUserValue  string   `json:"business_or_user_value,omitempty"`
	CurrentBehavior      string   `json:"current_behavior,omitempty"`
	DesiredBehavior      string   `json:"desired_behavior,omitempty"`
	ExplicitNonGoals     []string `json:"explicit_non_goals,omitempty"`
	Assumptions          []string `json:"assumptions,omitempty"`
	MissingInformation   []string `json:"missing_information,omitempty"`
	Contradictions       []string `json:"contradictions,omitempty"`
	ConfidenceLevel      string   `json:"confidence_level,omitempty"`
}

// BuildContextDossierOutput is build_context_dossier's output.
type BuildContextDossierOutput struct {
	Id      string                                 `json:"id"`
	Dossier protocol.KnowledgeRecordContextDossier `json:"dossier"`
}

func buildContextDossierHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, BuildContextDossierInput) (*mcp.CallToolResult, BuildContextDossierOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in BuildContextDossierInput) (*mcp.CallToolResult, BuildContextDossierOutput, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, BuildContextDossierOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		rec, err := dossier.Build(ctx, a.Workspace, a.Supervisor, store, dossier.BuildInput{
			WorkspaceID:          a.Workspace.ID,
			RunID:                in.RunId,
			AffectedRepositories: in.AffectedRepositories,
			UserGoal:             in.UserGoal,
			BusinessOrUserValue:  in.BusinessOrUserValue,
			CurrentBehavior:      in.CurrentBehavior,
			DesiredBehavior:      in.DesiredBehavior,
			ExplicitNonGoals:     in.ExplicitNonGoals,
			Assumptions:          in.Assumptions,
			MissingInformation:   in.MissingInformation,
			Contradictions:       in.Contradictions,
			ConfidenceLevel:      in.ConfidenceLevel,
		})
		if err != nil {
			return nil, BuildContextDossierOutput{}, fmt.Errorf("mcpserver: build context dossier: %w", err)
		}

		if err := store.Put(rec); err != nil {
			return nil, BuildContextDossierOutput{}, fmt.Errorf("mcpserver: persist context dossier: %w", err)
		}

		return nil, BuildContextDossierOutput{Id: rec.Id, Dossier: *rec.ContextDossier}, nil
	}
}
