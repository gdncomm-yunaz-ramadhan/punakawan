// Package capsule computes the deterministic content digest for a
// protocol.ContextCapsule, per punakawan-architecture-enhancement-plan.md
// §6.3 (AEP-M1, punokawan-ag2).
package capsule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// digestPayload is the exact, fixed-order subset of a ContextCapsule that
// Digest hashes, per §6.3: role, objective, requirements,
// acceptance_criteria, constraints, relevant_knowledge, evidence,
// allowed_tools, forbidden_actions. id/digest/task_id/created_at/
// expected_output/token_budget/assumptions/unresolved_questions are
// deliberately excluded, so two capsules built for the same task from the
// same substantive inputs hash identically regardless of when they were
// dispatched or what open questions they happened to record.
//
// Field order here is significant: encoding/json serializes a struct's
// fields in declaration order (unlike a map, whose key order is not
// guaranteed stable across Go versions), so this alone gives a stable byte
// sequence to hash without a separate canonicalization pass.
type digestPayload struct {
	Role               string                                         `json:"role"`
	Objective          string                                         `json:"objective"`
	Requirements       []protocol.ContextCapsuleRequirementsElem      `json:"requirements"`
	AcceptanceCriteria []string                                       `json:"acceptance_criteria"`
	Constraints        []string                                       `json:"constraints"`
	RelevantKnowledge  []protocol.ContextCapsuleRelevantKnowledgeElem `json:"relevant_knowledge"`
	Evidence           []protocol.ContextCapsuleEvidenceElem          `json:"evidence"`
	AllowedTools       []string                                       `json:"allowed_tools"`
	ForbiddenActions   []string                                       `json:"forbidden_actions"`
}

// Digest computes c's deterministic digest as "sha256:<hex>", matching
// internal/knowledge.ContentHash's format. A nil slice and an explicitly
// empty slice for the same field must hash identically - a caller that
// passes []string{} instead of leaving a field nil is not making a
// substantive change - so every slice is normalized to non-nil before
// marshaling.
func Digest(c protocol.ContextCapsule) (string, error) {
	payload := digestPayload{
		Role:               string(c.Role),
		Objective:          c.Objective,
		Requirements:       nonNil(c.Requirements),
		AcceptanceCriteria: nonNil(c.AcceptanceCriteria),
		Constraints:        nonNil(c.Constraints),
		RelevantKnowledge:  nonNil(c.RelevantKnowledge),
		Evidence:           nonNil(c.Evidence),
		AllowedTools:       nonNil(c.AllowedTools),
		ForbiddenActions:   nonNil(c.ForbiddenActions),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("capsule: marshal digest payload: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// nonNil returns s unchanged if it is already non-nil, or a non-nil empty
// slice of the same type otherwise, so json.Marshal emits "[]" rather than
// "null" for an unset field.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
