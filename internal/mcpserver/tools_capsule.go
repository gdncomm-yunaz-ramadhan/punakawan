package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/capsule"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// RequestCapsuleInput is request_capsule's input.
type RequestCapsuleInput struct {
	TaskId    string `json:"task_id"`
	Role      string `json:"role" jsonschema:"one of gareng|petruk|bagong"`
	Objective string `json:"objective"`

	RequirementIds []string `json:"requirement_ids,omitempty" jsonschema:"knowledge-record ids to cite as this capsule's requirements; rejected if any record's type is another role's output (§5's Context Rules)"`
	KnowledgeIds   []string `json:"knowledge_ids,omitempty" jsonschema:"knowledge-record ids to cite as relevant_knowledge; same per-role rejection as requirement_ids"`
	EvidenceIds    []string `json:"evidence_ids,omitempty" jsonschema:"opaque evidence references (e.g. an evidence-bundle path or EvidenceRecord id); not subject to per-role rejection since evidence is observation, not another role's reasoning"`

	AcceptanceCriteria  []string `json:"acceptance_criteria,omitempty"`
	Constraints         []string `json:"constraints,omitempty"`
	Assumptions         []string `json:"assumptions,omitempty"`
	UnresolvedQuestions []string `json:"unresolved_questions,omitempty"`

	AllowedTools     []string `json:"allowed_tools,omitempty" jsonschema:"rejected if it names a tool ForbiddenTools disallows for role, e.g. write_file for bagong"`
	ForbiddenActions []string `json:"forbidden_actions,omitempty"`

	ExpectedOutput string `json:"expected_output,omitempty"`
	TokenBudget    *int   `json:"token_budget,omitempty"`
}

func requestCapsuleHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RequestCapsuleInput) (*mcp.CallToolResult, protocol.ContextCapsule, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RequestCapsuleInput) (*mcp.CallToolResult, protocol.ContextCapsule, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, protocol.ContextCapsule{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		role := protocol.ContextCapsuleRole(in.Role)
		id := fmt.Sprintf("cap-%s-%s-%d", role, in.TaskId, time.Now().UnixNano())

		c, err := capsule.Build(store, id, time.Now().UTC(), capsule.BuildInput{
			TaskID:              in.TaskId,
			Role:                role,
			Objective:           in.Objective,
			RequirementIDs:      in.RequirementIds,
			KnowledgeIDs:        in.KnowledgeIds,
			EvidenceIDs:         in.EvidenceIds,
			AcceptanceCriteria:  in.AcceptanceCriteria,
			Constraints:         in.Constraints,
			Assumptions:         in.Assumptions,
			UnresolvedQuestions: in.UnresolvedQuestions,
			AllowedTools:        in.AllowedTools,
			ForbiddenActions:    in.ForbiddenActions,
			ExpectedOutput:      in.ExpectedOutput,
			TokenBudget:         in.TokenBudget,
		})
		if err != nil {
			return nil, protocol.ContextCapsule{}, fmt.Errorf("mcpserver: build capsule: %w", err)
		}

		if err := a.Capsules.Put(c); err != nil {
			return nil, protocol.ContextCapsule{}, fmt.Errorf("mcpserver: persist capsule: %w", err)
		}
		return nil, c, nil
	}
}

// requireCapsuleForRole loads capsuleID, verifies it exists, was issued for
// wantRole, and its stored digest still matches its own content (an
// integrity check against a capsule record edited after issuance - Get
// only ever returns exactly what Put stored, so a mismatch here would mean
// the stored bytes were tampered with outside punakawan). Submitting a
// role's review/plan without a matching capsule is exactly the gap
// punokawan-ow9 closes: previously nothing stopped a submission built from
// context the role should never have seen.
func requireCapsuleForRole(a *app.App, capsuleID string, wantRole protocol.ContextCapsuleRole) (protocol.ContextCapsule, error) {
	c, err := a.Capsules.Get(capsuleID)
	if err != nil {
		return protocol.ContextCapsule{}, fmt.Errorf("mcpserver: capsule %q: %w; call request_capsule first", capsuleID, err)
	}
	if c.Role != wantRole {
		return protocol.ContextCapsule{}, fmt.Errorf("mcpserver: capsule %q was issued for role %q, not %q", capsuleID, c.Role, wantRole)
	}
	recomputed, err := capsule.Digest(c)
	if err != nil {
		return protocol.ContextCapsule{}, fmt.Errorf("mcpserver: recompute capsule %q digest: %w", capsuleID, err)
	}
	if recomputed != c.Digest {
		return protocol.ContextCapsule{}, fmt.Errorf("mcpserver: capsule %q digest mismatch (stored %q, recomputed %q); it may have been altered after issuance", capsuleID, c.Digest, recomputed)
	}
	return c, nil
}
