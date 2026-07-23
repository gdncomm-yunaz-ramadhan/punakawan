package capsule

import (
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// BuildInput carries everything Build needs to assemble one role's
// ContextCapsule for one task.
type BuildInput struct {
	TaskID string
	Role   protocol.ContextCapsuleRole
	// Objective is required by the schema.
	Objective string

	// RequirementIDs and KnowledgeIDs are knowledge-record ids to resolve
	// and cite in the capsule's requirements/relevant_knowledge fields
	// respectively. Both are checked against ForbiddenKnowledgeTypes(Role)
	// - citing another role's output record here is exactly the leakage
	// this package exists to prevent (punokawan-ow9).
	RequirementIDs []string
	KnowledgeIDs   []string

	// EvidenceIDs are opaque evidence references (e.g. an EvidenceRecord id
	// or evidence-bundle path), passed through uninspected: evidence is
	// factual observation, not another role's reasoning, so it is not
	// subject to ForbiddenKnowledgeTypes.
	EvidenceIDs []string

	// KnowledgeReasons optionally maps a KnowledgeIDs entry to why it was
	// selected (e.g. a search_knowledge match explanation), recorded on the
	// capsule's relevant_knowledge item. Entries not present here get no
	// reason - a caller manually citing an id doesn't need to explain why.
	KnowledgeReasons map[string]string

	AcceptanceCriteria  []string
	Constraints         []string
	Assumptions         []string
	UnresolvedQuestions []string

	// AllowedTools is checked against ForbiddenTools(Role) - e.g. a Bagong
	// capsule cannot declare write_file/bulk_create_files/commit_task.
	AllowedTools     []string
	ForbiddenActions []string

	ExpectedOutput string
	TokenBudget    *int
}

// Build resolves in's requirement/knowledge ids against store, rejecting
// any that don't exist or whose type is forbidden for in.Role, validates
// AllowedTools against ForbiddenTools(in.Role), and returns a fully
// populated, digested ContextCapsule ready for Store.Put. id and now are
// caller-supplied (rather than minted here) so callers control the id
// scheme and Build stays deterministic for testing.
func Build(store *knowledge.Store, id string, now time.Time, in BuildInput) (protocol.ContextCapsule, error) {
	if in.TaskID == "" {
		return protocol.ContextCapsule{}, fmt.Errorf("capsule: task id is required")
	}
	if in.Objective == "" {
		return protocol.ContextCapsule{}, fmt.Errorf("capsule: objective is required")
	}

	for _, tool := range in.AllowedTools {
		if IsForbiddenTool(in.Role, tool) {
			return protocol.ContextCapsule{}, fmt.Errorf("capsule: tool %q is forbidden for role %q", tool, in.Role)
		}
	}

	requirements, err := resolveRefs(store, in.Role, in.RequirementIDs)
	if err != nil {
		return protocol.ContextCapsule{}, err
	}
	knowledgeRefs, err := resolveRefs(store, in.Role, in.KnowledgeIDs)
	if err != nil {
		return protocol.ContextCapsule{}, err
	}
	evidence := make([]protocol.ContextCapsuleEvidenceElem, len(in.EvidenceIDs))
	for i, evID := range in.EvidenceIDs {
		evidence[i] = protocol.ContextCapsuleEvidenceElem{Id: evID}
	}

	c := protocol.ContextCapsule{
		Id:                  id,
		TaskId:              in.TaskID,
		CreatedAt:           now,
		Role:                in.Role,
		Objective:           in.Objective,
		Requirements:        toRequirementRefs(requirements),
		AcceptanceCriteria:  in.AcceptanceCriteria,
		Constraints:         in.Constraints,
		RelevantKnowledge:   toKnowledgeRefs(knowledgeRefs, in.KnowledgeReasons),
		Evidence:            evidence,
		Assumptions:         in.Assumptions,
		UnresolvedQuestions: in.UnresolvedQuestions,
		// AllowedTools and ForbiddenActions are required, non-nullable
		// arrays in the schema (unlike the other slice fields, which are
		// optional): a caller that supplies neither must still get "[]",
		// not JSON null, or the capsule fails the MCP tool's own output
		// schema validation.
		AllowedTools:     nonNil(in.AllowedTools),
		ForbiddenActions: nonNil(in.ForbiddenActions),
	}
	if in.ExpectedOutput != "" {
		c.ExpectedOutput = &in.ExpectedOutput
	}
	c.TokenBudget = in.TokenBudget

	digest, err := Digest(c)
	if err != nil {
		return protocol.ContextCapsule{}, err
	}
	c.Digest = digest
	return c, nil
}

// resolveRefs looks up each id in store, rejecting any whose type is
// forbidden for role, and returns the resolved records in the same order.
func resolveRefs(store *knowledge.Store, role protocol.ContextCapsuleRole, ids []string) ([]protocol.KnowledgeRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	recs := make([]protocol.KnowledgeRecord, len(ids))
	for i, id := range ids {
		rec, err := store.Get(id)
		if err != nil {
			return nil, fmt.Errorf("capsule: resolve %q: %w", id, err)
		}
		if IsForbiddenKnowledgeType(role, rec.Type) {
			return nil, fmt.Errorf("capsule: %q is a %s record, which role %q must not receive (§5's Context Rules)", id, rec.Type, role)
		}
		recs[i] = rec
	}
	return recs, nil
}

func toRequirementRefs(recs []protocol.KnowledgeRecord) []protocol.ContextCapsuleRequirementsElem {
	if recs == nil {
		return nil
	}
	out := make([]protocol.ContextCapsuleRequirementsElem, len(recs))
	for i, r := range recs {
		out[i] = protocol.ContextCapsuleRequirementsElem{Id: r.Id, Summary: summaryPtr(r)}
	}
	return out
}

func toKnowledgeRefs(recs []protocol.KnowledgeRecord, reasons map[string]string) []protocol.ContextCapsuleRelevantKnowledgeElem {
	if recs == nil {
		return nil
	}
	out := make([]protocol.ContextCapsuleRelevantKnowledgeElem, len(recs))
	for i, r := range recs {
		ref := protocol.ContextCapsuleRelevantKnowledgeElem{Id: r.Id, Summary: summaryPtr(r)}
		if reason, ok := reasons[r.Id]; ok && reason != "" {
			ref.Reason = &reason
		}
		out[i] = ref
	}
	return out
}

func summaryPtr(r protocol.KnowledgeRecord) *string {
	if r.Title == "" {
		return nil
	}
	return &r.Title
}
