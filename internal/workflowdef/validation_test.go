package workflowdef

import (
	"errors"
	"testing"
)

func testCaps() CapabilitySet {
	return NewCapabilitySet(KnownMCPCapabilities(), []string{"knowledge.search", "jira.issue.search"})
}

func validDef() Definition {
	return Definition{
		Version: SchemaVersion,
		ID:      "wf",
		Name:    "WF",
		Steps: []Step{
			{ID: "a", Capability: "knowledge.search"},
			{ID: "b", Capability: "jira.issue.search", InputFrom: []string{"a"}},
		},
		AllowedCapabilities: []string{"knowledge.search", "jira.issue.search"},
	}
}

func TestValidateOK(t *testing.T) {
	if err := Validate(validDef(), testCaps()); err != nil {
		t.Fatalf("valid def rejected: %v", err)
	}
}

func TestValidateKnownMCPCapability(t *testing.T) {
	d := validDef()
	d.Steps = []Step{{ID: "a", Capability: "search_knowledge"}}
	d.AllowedCapabilities = []string{"search_knowledge"}
	if err := Validate(d, testCaps()); err != nil {
		t.Fatalf("MCP tool name should be a known capability: %v", err)
	}
}

func TestValidateUnknownCapability(t *testing.T) {
	d := validDef()
	d.Steps[0].Capability = "not.a.capability"
	err := Validate(d, testCaps())
	if !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("want ErrUnknownCapability, got %v", err)
	}
	// Also caught in allowed_capabilities.
	d = validDef()
	d.AllowedCapabilities = append(d.AllowedCapabilities, "bogus.cap")
	if !errors.Is(Validate(d, testCaps()), ErrUnknownCapability) {
		t.Fatalf("unknown allowed_capability not caught")
	}
}

func TestValidateCommandRejected(t *testing.T) {
	for _, bad := range []string{"rm -rf /", "a; b", "cat foo | grep bar", "x && y"} {
		d := validDef()
		d.Steps[0].Capability = bad
		if !errors.Is(Validate(d, testCaps()), ErrCommandNotAllowed) {
			t.Fatalf("command %q not rejected", bad)
		}
	}
}

func TestValidateBadVersion(t *testing.T) {
	d := validDef()
	d.Version = "punakawan.workflow/v2"
	if !errors.Is(Validate(d, testCaps()), ErrBadVersion) {
		t.Fatalf("bad version not caught")
	}
}

func TestValidateMissingFields(t *testing.T) {
	for _, mut := range []func(*Definition){
		func(d *Definition) { d.ID = "" },
		func(d *Definition) { d.Name = "" },
		func(d *Definition) { d.Version = "" },
	} {
		d := validDef()
		mut(&d)
		if !errors.Is(Validate(d, testCaps()), ErrMissingField) {
			t.Fatalf("missing field not caught for %+v", d)
		}
	}
}

func TestValidateDuplicateStepID(t *testing.T) {
	d := validDef()
	d.Steps = []Step{
		{ID: "dup", Capability: "knowledge.search"},
		{ID: "dup", Capability: "jira.issue.search"},
	}
	if !errors.Is(Validate(d, testCaps()), ErrDuplicateStepID) {
		t.Fatalf("dup step id not caught")
	}
}

func TestValidateBadInputFrom(t *testing.T) {
	// Forward reference (b defined after a references it) must fail.
	d := validDef()
	d.Steps = []Step{
		{ID: "a", Capability: "knowledge.search", InputFrom: []string{"b"}},
		{ID: "b", Capability: "jira.issue.search"},
	}
	if !errors.Is(Validate(d, testCaps()), ErrUnknownStepRef) {
		t.Fatalf("forward input_from not caught")
	}
	// Reference to a non-existent step.
	d = validDef()
	d.Steps[1].InputFrom = []string{"ghost"}
	if !errors.Is(Validate(d, testCaps()), ErrUnknownStepRef) {
		t.Fatalf("unknown input_from not caught")
	}
}

func TestCapabilitySet(t *testing.T) {
	caps := testCaps()
	if !caps.Has("knowledge.search") || !caps.Has("create_pr") {
		t.Fatalf("expected memberships missing")
	}
	if caps.Has("nope") {
		t.Fatalf("unexpected membership")
	}
	if caps.Len() != len(KnownMCPCapabilities())+2 {
		t.Fatalf("Len mismatch: %d", caps.Len())
	}
}
