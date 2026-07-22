package capsule

import "github.com/ygrip/punakawan/pkg/protocol"

// ForbiddenKnowledgeTypes lists the knowledge record types a capsule for
// role must not cite in requirements/relevant_knowledge, per
// punakawan-architecture-enhancement-plan.md §5.2-§5.4's Context Rules:
// each role's own output record type is another role's "prior reasoning",
// which the plan explicitly withholds from downstream roles (Gareng does
// not receive Petruk's conclusions or Bagong's verdict; Petruk's initial
// planning capsule does not receive Gareng's analysis; Bagong receives
// none of the others' reasoning at all, evidence only). Semar has broad
// access per §5.1 and is not subject to this restriction, so it has no
// case here.
func ForbiddenKnowledgeTypes(role protocol.ContextCapsuleRole) []protocol.KnowledgeRecordType {
	switch role {
	case protocol.ContextCapsuleRoleGareng:
		return []protocol.KnowledgeRecordType{
			protocol.KnowledgeRecordTypePetrukPlan,
			protocol.KnowledgeRecordTypeBagongReview,
			protocol.KnowledgeRecordTypeSemarSynthesis,
		}
	case protocol.ContextCapsuleRolePetruk:
		return []protocol.KnowledgeRecordType{
			protocol.KnowledgeRecordTypeGarengReview,
			protocol.KnowledgeRecordTypeBagongReview,
		}
	case protocol.ContextCapsuleRoleBagong:
		return []protocol.KnowledgeRecordType{
			protocol.KnowledgeRecordTypeGarengReview,
			protocol.KnowledgeRecordTypePetrukPlan,
			protocol.KnowledgeRecordTypeSemarSynthesis,
			protocol.KnowledgeRecordTypeBagongReview,
		}
	default:
		return nil
	}
}

// IsForbiddenKnowledgeType reports whether typ is disallowed for role.
func IsForbiddenKnowledgeType(role protocol.ContextCapsuleRole, typ protocol.KnowledgeRecordType) bool {
	for _, forbidden := range ForbiddenKnowledgeTypes(role) {
		if forbidden == typ {
			return true
		}
	}
	return false
}

// ForbiddenTools lists tool names a capsule for role must not declare in
// allowed_tools, per the plan's Non-Goals ("Allow Bagong to implement
// fixes"): Bagong verifies, it does not write.
func ForbiddenTools(role protocol.ContextCapsuleRole) []string {
	switch role {
	case protocol.ContextCapsuleRoleBagong:
		return []string{"write_file", "bulk_create_files", "commit_task"}
	default:
		return nil
	}
}

// IsForbiddenTool reports whether tool is disallowed for role.
func IsForbiddenTool(role protocol.ContextCapsuleRole, tool string) bool {
	for _, forbidden := range ForbiddenTools(role) {
		if forbidden == tool {
			return true
		}
	}
	return false
}
