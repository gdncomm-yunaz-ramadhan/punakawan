package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
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

		rec, err := roles.SubmitFinalPlan(store, recordID(a, "plan", in.Id), in.Title, *in.FinalPlan)
		if err != nil {
			return nil, SubmitOutput{}, err
		}
		return nil, SubmitOutput{Id: rec.Id, Type: rec.Type}, nil
	}
}
