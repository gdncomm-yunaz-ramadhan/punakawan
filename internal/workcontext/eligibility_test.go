package workcontext

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestClassifyValidity(t *testing.T) {
	cases := map[protocol.KnowledgeRecordValidityState]Eligibility{
		protocol.KnowledgeRecordValidityStateVerified:   Eligible,
		protocol.KnowledgeRecordValidityStateObserved:   Eligible,
		protocol.KnowledgeRecordValidityStateInferred:   Caution,
		protocol.KnowledgeRecordValidityStateAssumed:    OnRequest,
		protocol.KnowledgeRecordValidityStateDraft:      Excluded,
		protocol.KnowledgeRecordValidityStateValidating: Excluded,
		protocol.KnowledgeRecordValidityStateDisputed:   Excluded,
		protocol.KnowledgeRecordValidityStateStale:      Excluded,
		protocol.KnowledgeRecordValidityStateSuperseded: Excluded,
		protocol.KnowledgeRecordValidityStateInvalid:    Excluded,
	}
	for state, want := range cases {
		if got := ClassifyValidity(state); got != want {
			t.Errorf("ClassifyValidity(%q) = %v, want %v", state, got, want)
		}
	}
	if ClassifyValidity(protocol.KnowledgeRecordValidityState("bogus")) != Excluded {
		t.Error("unknown state should default to Excluded")
	}
}

func TestIncludeAsAccepted(t *testing.T) {
	if !IncludeAsAccepted(Eligible, false) {
		t.Error("Eligible should always be accepted")
	}
	if IncludeAsAccepted(Caution, true) {
		t.Error("Caution must never be accepted guidance")
	}
	if IncludeAsAccepted(OnRequest, false) {
		t.Error("OnRequest must not be accepted unless requested")
	}
	if !IncludeAsAccepted(OnRequest, true) {
		t.Error("OnRequest should be accepted when requested")
	}
	if IncludeAsAccepted(Excluded, true) {
		t.Error("Excluded must never be accepted")
	}
}
