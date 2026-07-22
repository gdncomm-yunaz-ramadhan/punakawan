package capsule

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestForbiddenKnowledgeTypesPerRole(t *testing.T) {
	cases := []struct {
		role      protocol.ContextCapsuleRole
		forbidden protocol.KnowledgeRecordType
		allowed   protocol.KnowledgeRecordType
	}{
		{protocol.ContextCapsuleRoleGareng, protocol.KnowledgeRecordTypePetrukPlan, protocol.KnowledgeRecordTypeRequirement},
		{protocol.ContextCapsuleRoleGareng, protocol.KnowledgeRecordTypeBagongReview, protocol.KnowledgeRecordTypeRequirement},
		{protocol.ContextCapsuleRolePetruk, protocol.KnowledgeRecordTypeGarengReview, protocol.KnowledgeRecordTypeRequirement},
		{protocol.ContextCapsuleRoleBagong, protocol.KnowledgeRecordTypeGarengReview, protocol.KnowledgeRecordTypeEvidence},
		{protocol.ContextCapsuleRoleBagong, protocol.KnowledgeRecordTypePetrukPlan, protocol.KnowledgeRecordTypeEvidence},
		{protocol.ContextCapsuleRoleBagong, protocol.KnowledgeRecordTypeSemarSynthesis, protocol.KnowledgeRecordTypeEvidence},
		{protocol.ContextCapsuleRoleBagong, protocol.KnowledgeRecordTypeBagongReview, protocol.KnowledgeRecordTypeEvidence},
	}
	for _, c := range cases {
		if !IsForbiddenKnowledgeType(c.role, c.forbidden) {
			t.Errorf("IsForbiddenKnowledgeType(%q, %q) = false, want true", c.role, c.forbidden)
		}
		if IsForbiddenKnowledgeType(c.role, c.allowed) {
			t.Errorf("IsForbiddenKnowledgeType(%q, %q) = true, want false", c.role, c.allowed)
		}
	}
}

func TestForbiddenToolsBagongCannotWrite(t *testing.T) {
	for _, tool := range []string{"write_file", "bulk_create_files", "commit_task"} {
		if !IsForbiddenTool(protocol.ContextCapsuleRoleBagong, tool) {
			t.Errorf("IsForbiddenTool(bagong, %q) = false, want true", tool)
		}
	}
	if IsForbiddenTool(protocol.ContextCapsuleRoleBagong, "run_tests") {
		t.Error("IsForbiddenTool(bagong, run_tests) = true, want false")
	}
	if IsForbiddenTool(protocol.ContextCapsuleRolePetruk, "write_file") {
		t.Error("IsForbiddenTool(petruk, write_file) = true, want false - petruk is the implementer")
	}
}
