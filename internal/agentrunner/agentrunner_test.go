package agentrunner

import (
	"context"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/agentpolicy"
)

func TestCapabilitiesReflectsDeclaredCapabilities(t *testing.T) {
	cfg := agentpolicy.Defaults()
	cfg.Capabilities = agentpolicy.DeclaredCapabilities{Fork: true, ReasoningControl: true}

	r := CapabilityRunner{
		ProjectID: "proj-1",
		Load:      func(projectID string) (*agentpolicy.Config, error) { return &cfg, nil },
	}

	got := r.Capabilities(context.Background())
	want := Capabilities{Fork: true, ReasoningControl: true}
	if got != want {
		t.Errorf("Capabilities() = %+v, want %+v", got, want)
	}
}

func TestCapabilitiesFailsClosedOnLoadError(t *testing.T) {
	r := CapabilityRunner{ProjectID: "proj-1"} // no Load configured
	got := r.Capabilities(context.Background())
	if got != (Capabilities{}) {
		t.Errorf("expected an unreadable policy to report no capabilities, got %+v", got)
	}
}

func TestRunRejectsMismatchedProject(t *testing.T) {
	cfg := agentpolicy.Defaults()
	r := CapabilityRunner{
		ProjectID: "proj-1",
		Load:      func(projectID string) (*agentpolicy.Config, error) { return &cfg, nil },
	}
	_, err := r.Run(context.Background(), Request{Purpose: PurposeReview, ProjectID: "proj-2"})
	if err == nil {
		t.Fatal("expected an error for a request whose ProjectID does not match the bound runner")
	}
}

func TestRunNamesMissingCapability(t *testing.T) {
	cases := []struct {
		name    string
		caps    agentpolicy.DeclaredCapabilities
		req     Request
		wantSub string
	}{
		{
			name:    "isolated context not declared",
			caps:    agentpolicy.DeclaredCapabilities{},
			req:     Request{Purpose: PurposeReview, ProjectID: "proj-1", Isolated: true},
			wantSub: "isolated_context",
		},
		{
			name:    "model selection not declared",
			caps:    agentpolicy.DeclaredCapabilities{},
			req:     Request{Purpose: PurposeReview, ProjectID: "proj-1", Model: "opus"},
			wantSub: "model_selection",
		},
		{
			name:    "reasoning control not declared",
			caps:    agentpolicy.DeclaredCapabilities{},
			req:     Request{Purpose: PurposeReview, ProjectID: "proj-1", Reasoning: "high"},
			wantSub: "reasoning_control",
		},
		{
			name:    "fork not declared for a fork-strategy purpose",
			caps:    agentpolicy.DeclaredCapabilities{},
			req:     Request{Purpose: PurposeImplement, ProjectID: "proj-1"},
			wantSub: "fork",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := agentpolicy.Defaults()
			cfg.Capabilities = tc.caps
			r := CapabilityRunner{
				ProjectID: "proj-1",
				Load:      func(projectID string) (*agentpolicy.Config, error) { return &cfg, nil },
			}
			_, err := r.Run(context.Background(), tc.req)
			if err == nil {
				t.Fatalf("expected an error, got none")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not mention missing capability %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestRunModelInheritNeedsNoCapability(t *testing.T) {
	cfg := agentpolicy.Defaults()
	cfg.Capabilities = agentpolicy.DeclaredCapabilities{} // nothing declared
	r := CapabilityRunner{
		ProjectID: "proj-1",
		Load:      func(projectID string) (*agentpolicy.Config, error) { return &cfg, nil },
	}
	_, err := r.Run(context.Background(), Request{Purpose: PurposeOrchestrate, Model: "inherit"})
	if err == nil {
		t.Fatal("expected Run to still fail (no execution engine), but got no error at all")
	}
	if strings.Contains(err.Error(), "model_selection") {
		t.Errorf("Model=\"inherit\" should not require model_selection, got error: %v", err)
	}
}

func TestRunWithEveryCapabilityDeclaredStillRefusesToExecute(t *testing.T) {
	cfg := agentpolicy.Defaults()
	cfg.Capabilities = agentpolicy.DeclaredCapabilities{
		Fork: true, ModelSelection: true, ReasoningControl: true, IsolatedContext: true,
	}
	r := CapabilityRunner{
		ProjectID: "proj-1",
		Load:      func(projectID string) (*agentpolicy.Config, error) { return &cfg, nil },
	}

	_, err := r.Run(context.Background(), Request{
		Purpose: PurposeImplement, ProjectID: "proj-1",
		Model: "opus", Reasoning: "high", Isolated: true,
	})
	if err == nil {
		t.Fatal("expected Run to always refuse, even when every capability is declared")
	}
	if strings.Contains(err.Error(), "does not support") {
		t.Errorf("expected the no-execution-engine error, not a capability-mismatch error: %v", err)
	}
}
