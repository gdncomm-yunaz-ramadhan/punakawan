package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/roles"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// recordID builds the pkw:<kind>/<workspace>/<localID> id (§6.2) for a role
// submission. Callers only supply the short local id; the server fills in
// the workspace segment itself so a client cannot submit a record under
// the wrong workspace by mistake.
func recordID(a *app.App, kind, localID string) string {
	return fmt.Sprintf("pkw:%s/%s/%s", kind, a.Workspace.ID, localID)
}

// SubmitGarengReviewInput is submit_gareng_review's input.
type SubmitGarengReviewInput struct {
	Id        string                               `json:"id" jsonschema:"short local id for this review, e.g. the run id"`
	CapsuleId string                               `json:"capsule_id" jsonschema:"the request_capsule id (role gareng) this review was produced under"`
	Title     string                               `json:"title" jsonschema:"human-readable title"`
	Review    protocol.KnowledgeRecordGarengReview `json:"review" jsonschema:"the Gareng review payload (§8.2)"`
}

// SubmitOutput is the common confirmation shape every submit_* tool returns.
type SubmitOutput struct {
	Id   string                       `json:"id"`
	Type protocol.KnowledgeRecordType `json:"type"`
}

func submitGarengReviewHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitGarengReviewInput) (*mcp.CallToolResult, SubmitOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitGarengReviewInput) (*mcp.CallToolResult, SubmitOutput, error) {
		if _, err := requireCapsuleForRole(a, in.CapsuleId, protocol.ContextCapsuleRoleGareng); err != nil {
			return nil, SubmitOutput{}, err
		}
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, SubmitOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}
		rec, err := roles.SubmitGarengReview(store, recordID(a, "gareng", in.Id), in.Title, in.Review)
		if err != nil {
			return nil, SubmitOutput{}, err
		}
		return nil, SubmitOutput{Id: rec.Id, Type: rec.Type}, nil
	}
}

// SubmitPetrukPlanInput is submit_petruk_plan's input.
type SubmitPetrukPlanInput struct {
	Id        string                             `json:"id" jsonschema:"short local id for this plan, e.g. the run id"`
	CapsuleId string                             `json:"capsule_id" jsonschema:"the request_capsule id (role petruk) this plan was produced under"`
	Title     string                             `json:"title" jsonschema:"human-readable title"`
	Plan      protocol.KnowledgeRecordPetrukPlan `json:"plan" jsonschema:"the Petruk planning payload (§8.3)"`
}

func submitPetrukPlanHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitPetrukPlanInput) (*mcp.CallToolResult, SubmitOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitPetrukPlanInput) (*mcp.CallToolResult, SubmitOutput, error) {
		if _, err := requireCapsuleForRole(a, in.CapsuleId, protocol.ContextCapsuleRolePetruk); err != nil {
			return nil, SubmitOutput{}, err
		}
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, SubmitOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}
		rec, err := roles.SubmitPetrukPlan(store, recordID(a, "petruk", in.Id), in.Title, in.Plan)
		if err != nil {
			return nil, SubmitOutput{}, err
		}
		return nil, SubmitOutput{Id: rec.Id, Type: rec.Type}, nil
	}
}

// SubmitBagongReviewInput is submit_bagong_review's input. RunId (unlike
// Gareng/Petruk/Semar's generic "Id") is a required, dedicated field: the
// advance_workflow completion gate (§18.1) looks up a run's Bagong review by
// this exact id, so it cannot be left to a "usually the run id" convention.
type SubmitBagongReviewInput struct {
	RunId     string                               `json:"run_id" jsonschema:"the workflow run id this review belongs to"`
	CapsuleId string                               `json:"capsule_id" jsonschema:"the request_capsule id (role bagong) this review was produced under"`
	Title     string                               `json:"title" jsonschema:"human-readable title"`
	Review    protocol.KnowledgeRecordBagongReview `json:"review" jsonschema:"the Bagong review payload (§8.4)"`
}

func submitBagongReviewHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitBagongReviewInput) (*mcp.CallToolResult, SubmitOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitBagongReviewInput) (*mcp.CallToolResult, SubmitOutput, error) {
		if _, err := requireCapsuleForRole(a, in.CapsuleId, protocol.ContextCapsuleRoleBagong); err != nil {
			return nil, SubmitOutput{}, err
		}
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, SubmitOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}
		rec, err := roles.SubmitBagongReview(store, recordID(a, "bagong", in.RunId), in.Title, in.Review)
		if err != nil {
			return nil, SubmitOutput{}, err
		}
		return nil, SubmitOutput{Id: rec.Id, Type: rec.Type}, nil
	}
}
