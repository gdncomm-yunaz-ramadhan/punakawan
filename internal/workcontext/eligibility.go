// Package workcontext is the single service that composes Punakawan's three
// project-context pillars — workflow, project metadata, and knowledge — into
// one bounded, immutable snapshot for a run (agent-context plan §4.4). It is
// deterministic and performs no reasoning: given the same inputs and the same
// store revisions it produces the same snapshot and the same digest, which is
// what lets a run be reproduced and a context improvement be attributed.
//
// Determinism is why context preparation *resolves* a retrieval recipe but
// never *executes* one: executing a recipe is a live external fetch (Jira,
// etc.) whose result varies over time, which would make the digest
// non-reproducible. Recipe execution stays with the agent's actual work.
package workcontext

import "github.com/ygrip/punakawan/pkg/protocol"

// Eligibility classifies how a knowledge record's validity state may be used
// when preparing context (agent-context plan §4.5). It is the deterministic
// gate that keeps unsafe states from ever being presented as accepted
// guidance.
type Eligibility int

const (
	// Excluded records must not appear as guidance at all (draft, validating,
	// disputed, stale, superseded, invalid). A disputed or stale record may
	// still be reported elsewhere as a warning/missing-context signal, but it
	// is never eligible context.
	Excluded Eligibility = iota
	// Eligible records are presented as accepted context (verified, observed).
	Eligible
	// Caution records (inferred) may appear only in a clearly marked caution
	// section, never mixed into accepted guidance.
	Caution
	// OnRequest records (assumed) appear only when the caller explicitly asks
	// for them.
	OnRequest
)

// ClassifyValidity maps a knowledge validity state to its context eligibility,
// implementing the plan §4.5 table exactly. An unrecognized state is treated
// as Excluded — the safe default is to withhold, not to leak.
func ClassifyValidity(state protocol.KnowledgeRecordValidityState) Eligibility {
	switch state {
	case protocol.KnowledgeRecordValidityStateVerified,
		protocol.KnowledgeRecordValidityStateObserved:
		return Eligible
	case protocol.KnowledgeRecordValidityStateInferred:
		return Caution
	case protocol.KnowledgeRecordValidityStateAssumed:
		return OnRequest
	case protocol.KnowledgeRecordValidityStateDraft,
		protocol.KnowledgeRecordValidityStateValidating,
		protocol.KnowledgeRecordValidityStateDisputed,
		protocol.KnowledgeRecordValidityStateStale,
		protocol.KnowledgeRecordValidityStateSuperseded,
		protocol.KnowledgeRecordValidityStateInvalid:
		return Excluded
	default:
		return Excluded
	}
}

// IncludeAsAccepted reports whether a record may be presented as accepted
// guidance: Eligible always, OnRequest only when includeAssumed is set.
// Caution records are deliberately excluded here — they belong in the separate
// caution channel, not accepted context.
func IncludeAsAccepted(e Eligibility, includeAssumed bool) bool {
	switch e {
	case Eligible:
		return true
	case OnRequest:
		return includeAssumed
	default:
		return false
	}
}
