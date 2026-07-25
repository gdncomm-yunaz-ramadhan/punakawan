package workflowdef

import (
	"context"
	"errors"
	"testing"
)

func TestInvokeHappy(t *testing.T) {
	caps := testCaps()
	var gotDef Definition
	var gotInputs map[string]any
	inv := NewInvoker(caps, func(_ context.Context, d Definition, in map[string]any) (string, error) {
		gotDef = d
		gotInputs = in
		return "run-123", nil
	})

	d := validDef()
	d.Enabled = true
	runID, err := inv.Invoke(context.Background(), d, map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if runID != "run-123" {
		t.Fatalf("run id = %q", runID)
	}
	if gotDef.ID != d.ID || gotInputs["k"] != "v" {
		t.Fatalf("RunCreator got wrong args: %+v %+v", gotDef, gotInputs)
	}
}

func TestInvokeDisabledRejected(t *testing.T) {
	called := false
	inv := NewInvoker(testCaps(), func(context.Context, Definition, map[string]any) (string, error) {
		called = true
		return "x", nil
	})
	d := validDef()
	d.Enabled = false
	_, err := inv.Invoke(context.Background(), d, nil)
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
	if called {
		t.Fatalf("RunCreator must not run for disabled def")
	}
}

func TestInvokeCapabilityRecheck(t *testing.T) {
	// A definition that references a capability absent from the (now
	// narrower) set must be rejected at invoke time even though it is enabled.
	caps := NewCapabilitySet(nil, []string{"knowledge.search"}) // jira.issue.search absent
	inv := NewInvoker(caps, func(context.Context, Definition, map[string]any) (string, error) {
		return "x", nil
	})
	d := validDef() // references jira.issue.search
	d.Enabled = true
	_, err := inv.Invoke(context.Background(), d, nil)
	if !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("want ErrUnknownCapability, got %v", err)
	}
}

func TestInvokeRunCreatorError(t *testing.T) {
	sentinel := errors.New("boom")
	inv := NewInvoker(testCaps(), func(context.Context, Definition, map[string]any) (string, error) {
		return "", sentinel
	})
	d := validDef()
	d.Enabled = true
	_, err := inv.Invoke(context.Background(), d, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("want wrapped sentinel, got %v", err)
	}
}
