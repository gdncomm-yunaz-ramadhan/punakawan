package delivery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeAdapterGate is a minimal adapterGate stub for exercising
// CheckGitHubRepositoryAccess/RunPreflight without a real spawned GitHub
// adapter subprocess, mirroring internal/mcpserver's own fake
// adapterGateProvider test pattern.
type fakeAdapterGate struct {
	response json.RawMessage
	err      error
}

func (f *fakeAdapterGate) Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.response, nil
}

func accessibleRepoGate(push bool) *fakeAdapterGate {
	raw, _ := json.Marshal(map[string]any{
		"normalized": map[string]any{
			"accessible":    true,
			"private":       true,
			"permissions":   map[string]any{"admin": false, "maintain": false, "push": push, "pull": true, "triage": false},
			"defaultBranch": "main",
		},
	})
	return &fakeAdapterGate{response: raw}
}

func inaccessibleRepoGate() *fakeAdapterGate {
	raw, _ := json.Marshal(map[string]any{
		"normalized": map[string]any{
			"accessible":    false,
			"private":       nil,
			"permissions":   nil,
			"defaultBranch": nil,
		},
	})
	return &fakeAdapterGate{response: raw}
}

func TestRunPreflightSkipsGitHubChecksWithoutAGate(t *testing.T) {
	profile := &protocol.ProjectDeliveryProfile{BaseBranch: "main"}

	checks := RunPreflight(context.Background(), profile, nil, nil, "")
	byName := map[string]protocol.PreflightCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	for _, name := range []string{"pr-permissions", "private-repository-identity", "ci-visibility"} {
		c, ok := byName[name]
		if !ok || c.Status != protocol.PreflightCheckStatusSkipped {
			t.Fatalf("expected %s to stay skipped with no github gate, got %+v", name, c)
		}
	}

	// Same behavior when a gate is present but no repository slug is
	// known yet - there is nothing to check without one.
	checksWithGateNoSlug := RunPreflight(context.Background(), profile, nil, accessibleRepoGate(true), "")
	for _, c := range checksWithGateNoSlug {
		if c.Name == "pr-permissions" && c.Status != protocol.PreflightCheckStatusSkipped {
			t.Fatalf("expected pr-permissions to stay skipped with no repo slug, got %+v", c)
		}
	}
}

func TestRunPreflightGitHubChecksPassForAccessibleRepoWithPush(t *testing.T) {
	profile := &protocol.ProjectDeliveryProfile{BaseBranch: "main"}
	checks := RunPreflight(context.Background(), profile, nil, accessibleRepoGate(true), "acme/widgets")

	byName := map[string]protocol.PreflightCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if c := byName["private-repository-identity"]; c.Status != protocol.PreflightCheckStatusPass {
		t.Fatalf("expected private-repository-identity to pass for an accessible repo, got %+v", c)
	}
	if c := byName["pr-permissions"]; c.Status != protocol.PreflightCheckStatusPass {
		t.Fatalf("expected pr-permissions to pass when push access is present, got %+v", c)
	}
}

func TestRunPreflightGitHubChecksFailWithoutPushAccess(t *testing.T) {
	profile := &protocol.ProjectDeliveryProfile{BaseBranch: "main"}
	checks := RunPreflight(context.Background(), profile, nil, accessibleRepoGate(false), "acme/widgets")

	byName := map[string]protocol.PreflightCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if c := byName["private-repository-identity"]; c.Status != protocol.PreflightCheckStatusPass {
		t.Fatalf("expected private-repository-identity to still pass (repo is accessible), got %+v", c)
	}
	if c := byName["pr-permissions"]; c.Status != protocol.PreflightCheckStatusFail {
		t.Fatalf("expected pr-permissions to fail without push access, got %+v", c)
	}
}

func TestRunPreflightGitHubChecksFailForInaccessibleRepo(t *testing.T) {
	profile := &protocol.ProjectDeliveryProfile{BaseBranch: "main"}
	checks := RunPreflight(context.Background(), profile, nil, inaccessibleRepoGate(), "acme/private-repo")

	byName := map[string]protocol.PreflightCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if c := byName["private-repository-identity"]; c.Status != protocol.PreflightCheckStatusFail {
		t.Fatalf("expected private-repository-identity to fail for an inaccessible repo, got %+v", c)
	}
	if c := byName["pr-permissions"]; c.Status != protocol.PreflightCheckStatusFail {
		t.Fatalf("expected pr-permissions to fail for an inaccessible repo, got %+v", c)
	}
}
