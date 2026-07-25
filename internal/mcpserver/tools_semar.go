package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/contradiction"
	"github.com/ygrip/punakawan/internal/roles"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SubmitSemarSynthesisInput is submit_semar_synthesis's input. Exactly one
// of Synthesis or FinalPlan must be set: Semar produces two distinct
// submission shapes at two distinct workflow stages (§8.1/§9.2 vs §9.3),
// and this single tool (per §28.4) routes to whichever the client sent.
type SubmitSemarSynthesisInput struct {
	Id        string                                  `json:"id" jsonschema:"short local id for this submission, e.g. the run id"`
	Title     string                                  `json:"title" jsonschema:"human-readable title"`
	Synthesis *protocol.KnowledgeRecordSemarSynthesis `json:"synthesis,omitempty" jsonschema:"the clarification-consolidation payload (§8.1/§9.2)"`
	FinalPlan *protocol.KnowledgeRecordFinalPlan      `json:"final_plan,omitempty" jsonschema:"the final implementation plan payload (§9.3)"`
}

// contradictionTitles renders a short "id (title)" list for an error message.
func contradictionTitles(cs []protocol.Contradiction) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, fmt.Sprintf("%s (%s)", c.Id, c.Title))
	}
	return strings.Join(parts, ", ")
}

func submitSemarSynthesisHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitSemarSynthesisInput) (*mcp.CallToolResult, SubmitOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitSemarSynthesisInput) (*mcp.CallToolResult, SubmitOutput, error) {
		if (in.Synthesis == nil) == (in.FinalPlan == nil) {
			return nil, SubmitOutput{}, fmt.Errorf("mcpserver: submit_semar_synthesis: exactly one of synthesis or final_plan must be set")
		}

		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, SubmitOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		if in.Synthesis != nil {
			rec, err := roles.SubmitSemarSynthesis(store, recordID(a, "synthesis", in.Id), in.Title, *in.Synthesis)
			if err != nil {
				return nil, SubmitOutput{}, err
			}
			return nil, SubmitOutput{Id: rec.Id, Type: rec.Type}, nil
		}

		// CONTRA-008: Semar may not finalize a plan while blocking
		// contradictions remain open. Surface them to the human instead of
		// silently proceeding. Best-effort: a store error here must not mask
		// the submission, so only a successful list with open blockers blocks.
		if a.Workspace.Root != "" {
			if blockers, lerr := contradiction.OpenBlocking(a.Workspace.Root); lerr == nil && len(blockers) > 0 {
				return nil, SubmitOutput{}, fmt.Errorf("mcpserver: submit_semar_synthesis: cannot finalize plan while %d blocking contradiction(s) are open: %s", len(blockers), contradictionTitles(blockers))
			}
		}

		rec, err := roles.SubmitFinalPlan(store, recordID(a, "plan", in.Id), in.Title, *in.FinalPlan)
		if err != nil {
			return nil, SubmitOutput{}, err
		}
		return nil, SubmitOutput{Id: rec.Id, Type: rec.Type}, nil
	}
}
