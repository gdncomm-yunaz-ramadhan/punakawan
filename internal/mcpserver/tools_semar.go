package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/contradiction"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubmitFinalPlanInput is submit_final_plan's input (§9.3).
type SubmitFinalPlanInput struct {
	Id        string                             `json:"id" jsonschema:"short local id for this submission, e.g. the run id"`
	Title     string                             `json:"title" jsonschema:"human-readable title"`
	FinalPlan *protocol.KnowledgeRecordFinalPlan `json:"final_plan" jsonschema:"the final implementation plan payload"`
}

// contradictionTitles renders a short "id (title)" list for an error message.
func contradictionTitles(cs []protocol.Contradiction) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, fmt.Sprintf("%s (%s)", c.Id, c.Title))
	}
	return strings.Join(parts, ", ")
}

// submitFinalPlanHandler is a thin compatibility wrapper (§4.4) over
// internal/plan.Store: it keeps submit_final_plan's existing
// final_plan-shaped input and SubmitOutput{Id,Type} output so every
// existing caller keeps working unchanged, but a submission now lands as
// a Plan revision instead of a fresh knowledge.Store record. For a
// caller that wants the fuller Plan shape (steps, project_ids,
// revisions), use plan_save/plan_get directly.
func submitFinalPlanHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitFinalPlanInput) (*mcp.CallToolResult, SubmitOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitFinalPlanInput) (*mcp.CallToolResult, SubmitOutput, error) {
		if in.FinalPlan == nil {
			return nil, SubmitOutput{}, fmt.Errorf("mcpserver: submit_final_plan: final_plan is required")
		}

		// CONTRA-008: Semar may not finalize a plan while blocking
		// contradictions remain open. Surface them to the human instead of
		// silently proceeding. Best-effort: a store error here must not mask
		// the submission, so only a successful list with open blockers blocks.
		if a.Workspace.Root != "" {
			if blockers, lerr := contradiction.OpenBlocking(a.Workspace.Root); lerr == nil && len(blockers) > 0 {
				return nil, SubmitOutput{}, fmt.Errorf("mcpserver: submit_final_plan: cannot finalize plan while %d blocking contradiction(s) are open: %s", len(blockers), contradictionTitles(blockers))
			}
		}

		p, err := plan.FromFinalPlanInput(recordID(a, "plan", in.Id), in.Title, *in.FinalPlan)
		if err != nil {
			return nil, SubmitOutput{}, err
		}
		p.CreatedBy = "punakawan-mcp"

		planStore, err := a.OpenPlan()
		if err != nil {
			return nil, SubmitOutput{}, fmt.Errorf("mcpserver: open plan store: %w", err)
		}
		saved, err := planStore.Save(ctx, p)
		if err != nil {
			return nil, SubmitOutput{}, err
		}
		return nil, SubmitOutput{Id: saved.ID, Type: protocol.KnowledgeRecordTypeFinalPlan}, nil
	}
}
